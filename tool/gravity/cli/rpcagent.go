/*
Copyright 2018 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gravitational/gravity/lib/constants"
	"github.com/gravitational/gravity/lib/defaults"
	"github.com/gravitational/gravity/lib/fsm"
	"github.com/gravitational/gravity/lib/loc"
	"github.com/gravitational/gravity/lib/localenv"
	"github.com/gravitational/gravity/lib/pack"
	"github.com/gravitational/gravity/lib/rpc"
	pb "github.com/gravitational/gravity/lib/rpc/proto"
	rpcserver "github.com/gravitational/gravity/lib/rpc/server"
	"github.com/gravitational/gravity/lib/storage"
	"github.com/gravitational/gravity/lib/update"
	clusterupdate "github.com/gravitational/gravity/lib/update/cluster"
	"github.com/gravitational/gravity/lib/utils"

	"github.com/cenkalti/backoff"
	teleclient "github.com/gravitational/teleport/lib/client"
	"github.com/gravitational/trace"
	"github.com/gravitational/version"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/credentials"
)

var cDialTimeout = 1 * time.Second

func rpcAgentInstall(env *localenv.LocalEnvironment, args []string) error {
	gravityPath, err := os.Executable()
	if err != nil {
		return trace.Wrap(err, "failed to determine gravity executable path")
	}

	return trace.Wrap(reinstallOneshotService(env,
		defaults.GravityRPCAgentServiceName,
		append([]string{gravityPath, "--debug", "agent", "run"}, args...)))
}

// rpcAgentRun runs a local agent executing the function specified with optional args
func rpcAgentRun(localEnv, updateEnv *localenv.LocalEnvironment, args []string) error {
	agent, err := newAgent()
	if err != nil {
		return trace.Wrap(err)
	}
	if len(args) == 0 {
		return trace.Wrap(agent.Serve())
	}
	agentFunc, exists := agentFunctions[args[0]]
	if !exists {
		return trace.NotFound("no such function %q", args[0])
	}
	return trace.Wrap(runAgentFunction(localEnv, updateEnv, agent, agentFunc, args))
}

func runAgentFunction(
	localEnv, updateEnv *localenv.LocalEnvironment,
	agent rpcserver.Server,
	agentFunc agentFunc,
	args []string,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error, 2)
	f := func() error {
		handler, args := args[0], args[1:]
		log.WithField("handler", handler).Info("Execute.")
		return trace.Wrap(agentFunc(ctx, localEnv, updateEnv, args))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaults.ShutdownTimeout)
		defer cancel()
		agent.Stop(ctx)
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		select {
		case errC <- agent.Serve():
		case <-ctx.Done():
		}
		wg.Done()
	}()
	go func() {
		select {
		case errC <- f():
		case <-ctx.Done():
		}
		wg.Done()
	}()
	err := <-errC
	cancel()
	wg.Wait()
	return trace.Wrap(err)
}

func newAgent() (rpcserver.Server, error) {
	secretsDir, err := fsm.AgentSecretsDir()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	serverCreds, clientCreds, err := rpc.Credentials(secretsDir)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	serverAddr := fmt.Sprintf(":%v", defaults.GravityRPCAgentPort)
	listener, err := net.Listen("tcp4", serverAddr)
	if err != nil {
		return nil, trace.Wrap(err, "failed to bind to %v")
	}

	config := rpcserver.Config{
		Credentials: rpcserver.Credentials{
			Server: serverCreds,
			Client: clientCreds,
		},
		Listener: listener,
	}
	server, err := rpcserver.New(config)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	log.Infof("Starting RPC agent on %v.", listener.Addr().String())

	return server, nil
}

type agentFunc func(ctx context.Context, localEnv, upgradeEnv *localenv.LocalEnvironment, args []string) error

var agentFunctions map[string]agentFunc = map[string]agentFunc{
	constants.RPCAgentUpgradeFunction:  executeAutomaticUpgrade,
	constants.RPCAgentSyncPlanFunction: executeSyncOperationPlan,
}

func rpcAgentDeploy(localEnv *localenv.LocalEnvironment, leaderParams, nodeParams string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaults.AgentDeployTimeout)
	defer cancel()
	_, err := rpcAgentDeployHelper(ctx, localEnv, leaderParams, nodeParams)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func rpcAgentDeployHelper(ctx context.Context, localEnv *localenv.LocalEnvironment, leaderParams, nodeParams string) (credentials.TransportCredentials, error) {
	localEnv.PrintStep("Deploying agents on the cluster nodes")

	clusterEnv, err := localEnv.NewClusterEnvironment()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	operator, err := localEnv.SiteOperator()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	cluster, err := operator.GetLocalSite()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	teleportClient, err := localEnv.TeleportClient(constants.Localhost)
	if err != nil {
		return nil, trace.Wrap(err, "failed to create a teleport client")
	}

	proxy, err := teleportClient.ConnectToProxy(ctx)
	if err != nil {
		return nil, trace.Wrap(err, "failed to connect to teleport proxy")
	}

	req := deployAgentsRequest{
		clusterState: cluster.ClusterState,
		clusterName:  cluster.Domain,
		clusterEnv:   clusterEnv,
		proxy:        proxy,
		leaderParams: leaderParams,
		nodeParams:   nodeParams,
	}

	// Force this node to be the operation leader
	req.leader, err = findLocalServer(*cluster)
	if err != nil {
		log.WithError(err).Warn("Failed to determine local node.")
		return nil, trace.Wrap(err, "failed to find local node in cluster state.\n"+
			"Make sure you start the operation from one of the cluster master nodes.")
	}

	localCtx, cancel := context.WithTimeout(ctx, defaults.AgentDeployTimeout)
	defer cancel()
	return deployAgents(localCtx, req)
}

func verifyCluster(ctx context.Context,
	clusterState storage.ClusterState,
	proxy *teleclient.ProxyClient,
) (servers []rpc.DeployServer, err error) {
	var missing []string
	servers = make([]rpc.DeployServer, 0, len(servers))

	for _, server := range clusterState.Servers {
		deployServer := rpc.NewDeployServer(server)
		// do a quick check to make sure we can connect to the teleport node
		client, err := proxy.ConnectToNode(ctx, deployServer.NodeAddr,
			defaults.SSHUser, false)
		if err != nil {
			log.Errorf("Failed to connect to teleport on node %v: %v.",
				deployServer, trace.DebugReport(err))
			missing = append(missing, server.Hostname)
		} else {
			client.Close()
			log.Infof("Successfully connected to teleport on node %v (%v).",
				server.Hostname, deployServer.NodeAddr)
			servers = append(servers, deployServer)
		}
	}
	if len(missing) != 0 {
		return nil, trace.NotFound(
			"Teleport is unavailable "+
				"on the following cluster nodes: %s. Please "+
				"make sure that the Teleport service is running "+
				"and try again.", strings.Join(missing, ", "))
	}

	return servers, nil
}

func upsertRPCCredentialsPackage(
	servers []rpc.DeployServer,
	packages pack.PackageService,
	clusterName string,
	packageTemplate loc.Locator) (secretsPackage *loc.Locator, err error) {
	hosts := make([]string, 0, len(servers))
	for _, server := range servers {
		hosts = append(hosts, strings.Split(server.NodeAddr, ":")[0])
	}

	archive, err := rpc.GenerateAgentCredentials(hosts, clusterName, false)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	secretsPackage, err = rpc.GenerateAgentCredentialsPackage(packages, packageTemplate, archive)
	if err != nil && !trace.IsAlreadyExists(err) {
		return nil, trace.Wrap(err)
	}
	return secretsPackage, nil
}

func deployAgents(ctx context.Context, req deployAgentsRequest) (credentials.TransportCredentials, error) {
	deployReq, err := newDeployAgentsRequest(ctx, req)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	err = rpc.DeployAgents(ctx, *deployReq)
	if err != nil {
		return nil, trace.Wrap(err, "failed to deploy agents")
	}

	clientCreds, err := getClientCredentials(ctx, req.clusterEnv.ClusterPackages, deployReq.SecretsPackage)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return clientCreds, nil
}

// newDeployAgentsRequest creates a new request to deploy agents on the local cluster
func newDeployAgentsRequest(ctx context.Context, req deployAgentsRequest) (*rpc.DeployAgentsRequest, error) {
	servers, err := verifyCluster(ctx, req.clusterState, req.proxy)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	gravityPackage := getGravityPackage()
	secretsPackageTemplate := loc.Locator{
		Repository: req.clusterName,
		Version:    gravityPackage.Version,
	}
	secretsPackage, err := upsertRPCCredentialsPackage(
		servers, req.clusterEnv.ClusterPackages, req.clusterName, secretsPackageTemplate)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &rpc.DeployAgentsRequest{
		Proxy:          req.proxy,
		ClusterState:   req.clusterState,
		Servers:        servers,
		SecretsPackage: *secretsPackage,
		GravityPackage: gravityPackage,
		FieldLogger:    logrus.WithField(trace.Component, "rpc:deploy"),
		LeaderParams:   req.leaderParams,
		Leader:         req.leader,
		NodeParams:     req.nodeParams,
	}, nil
}

func getClientCredentials(ctx context.Context, packages pack.PackageService, secretsPackage loc.Locator) (credentials.TransportCredentials, error) {
	var r io.Reader
	ctx, cancel := defaults.WithTimeout(ctx)
	defer cancel()
	err := utils.RetryWithInterval(ctx, utils.NewUnlimitedExponentialBackOff(), func() (err error) {
		_, r, err = packages.ReadPackage(secretsPackage)
		if err != nil {
			if utils.IsPathError(err) {
				log.Debugf("Package %v has not been replicated yet, will retry.", secretsPackage)
				return trace.Wrap(err)
			}
			return &backoff.PermanentError{Err: err}
		}
		return nil
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	tlsArchive, err := utils.ReadTLSArchive(r)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	clientCreds, err := rpc.ClientCredentialsFromKeyPairs(
		*tlsArchive[pb.Client], *tlsArchive[pb.CA])
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return clientCreds, nil
}

func rpcAgentShutdown(env *localenv.LocalEnvironment) error {
	env.PrintStep("Shutting down the agents")
	creds, err := fsm.GetClientCredentials()
	if err != nil {
		return trace.Wrap(err)
	}
	runner := fsm.NewAgentRunner(creds)
	err = clusterupdate.ShutdownClusterAgents(context.TODO(), runner)
	return trace.Wrap(err)
}

func executeAutomaticUpgrade(ctx context.Context, localEnv, upgradeEnv *localenv.LocalEnvironment, args []string) error {
	return trace.Wrap(clusterupdate.AutomaticUpgrade(ctx, localEnv, upgradeEnv))
}

func executeSyncOperationPlan(ctx context.Context, localEnv, updateEnv *localenv.LocalEnvironment, args []string) error {
	clusterEnv, err := localEnv.NewClusterEnvironment()
	if err != nil {
		return trace.Wrap(err)
	}
	operation, err := storage.GetLastOperation(clusterEnv.Backend)
	if err != nil {
		return trace.Wrap(err)
	}
	plan, err := clusterEnv.Backend.GetOperationPlan(operation.SiteDomain, operation.ID)
	if err != nil {
		return trace.Wrap(err)
	}
	return trace.Wrap(update.SyncOperationPlan(clusterEnv.Backend, updateEnv.Backend, *plan, *operation))
}

func getGravityPackage() loc.Locator {
	ver := version.Get()
	return loc.Locator{
		Repository: defaults.SystemAccountOrg,
		Name:       constants.GravityPackage,
		Version:    strings.Split(ver.Version, "+")[0],
	}
}

type deployAgentsRequest struct {
	clusterEnv   *localenv.ClusterEnvironment
	clusterState storage.ClusterState
	clusterName  string
	proxy        *teleclient.ProxyClient
	leaderParams string
	leader       *storage.Server
	nodeParams   string
}
