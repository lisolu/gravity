/*
Copyright 2020 Gravitational, Inc.

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

	"github.com/gravitational/gravity/lib/install"
	installerclient "github.com/gravitational/gravity/lib/install/client"
	"github.com/gravitational/gravity/lib/install/reconfigure"
	"github.com/gravitational/gravity/lib/localenv"
	"github.com/gravitational/gravity/lib/system/signals"
	"github.com/gravitational/gravity/lib/utils"
	"github.com/gravitational/gravity/lib/utils/cli"

	"github.com/gravitational/trace"
)

// reconfigureCluster starts the cluster reconfiguration operation.
//
// Currently, the reconfiguration operation only allows to change advertise
// address for single-node clusters.
func reconfigureCluster(env *localenv.LocalEnvironment, config InstallConfig) error {
	env.PrintStep("Starting reconfigurator")
	// Determine the existing cluster name.
	localState, err := reconfigure.GetLocalState(env.Packages)
	if err != nil {
		return trace.Wrap(err)
	}
	log.Infof("Determined local cluster state: %#v.", localState)
	if err := reconfigure.ValidateLocalState(localState); err != nil {
		return trace.Wrap(err)
	}
	config.SiteDomain = localState.Cluster.Domain
	if err := config.CheckAndSetDefaults(); err != nil {
		return trace.Wrap(err)
	}
	if config.FromService {
		err := startReconfiguratorFromService(env, config, localState)
		if utils.IsContextCancelledError(err) {
			return trace.Wrap(err, "reconfigurator interrupted")
		}
		return trace.Wrap(err)
	}
	strategy, err := newReconfiguratorConnectStrategy(env, config, cli.CommandArgs{
		Parser: cli.ArgsParserFunc(parseArgs),
	})
	if err != nil {
		return trace.Wrap(err)
	}
	err = InstallerClient(env, installerclient.Config{
		ConnectStrategy: strategy,
		Lifecycle: &installerclient.AutomaticLifecycle{
			Aborter:            AborterForMode(config.Mode, env),
			Completer:          InstallerCompleteOperation(env),
			DebugReportPath:    DebugReportPath(),
			LocalDebugReporter: InstallerGenerateLocalReport(env),
		},
	})
	if utils.IsContextCancelledError(err) {
		InstallerCleanup()
		return trace.Wrap(err, "reconfigurator interrupted")
	}
	return trace.Wrap(err)
}

func startReconfiguratorFromService(env *localenv.LocalEnvironment, config InstallConfig, state *reconfigure.State) error {
	ctx, cancel := context.WithCancel(context.Background())
	interrupt := signals.NewInterruptHandler(ctx, cancel, InterruptSignals)
	defer interrupt.Close()
	go TerminationHandler(interrupt, env)
	listener, err := NewServiceListener()
	if err != nil {
		return trace.Wrap(utils.NewPreconditionFailedError(err))
	}
	defer func() {
		if err != nil {
			listener.Close()
		}
	}()
	installerConfig, err := newInstallerConfig(ctx, env, config)
	if err != nil {
		return trace.Wrap(utils.NewPreconditionFailedError(err))
	}
	installer, err := newReconfigurator(ctx, installerConfig, state)
	if err != nil {
		return trace.Wrap(utils.NewPreconditionFailedError(err))
	}
	interrupt.AddStopper(installer)
	return trace.Wrap(installer.Run(listener))
}

func newReconfigurator(ctx context.Context, config *install.Config, state *reconfigure.State) (*install.Installer, error) {
	engine, err := reconfigure.NewEngine(reconfigure.Config{
		Operator: config.Operator,
		State:    state,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	config.LocalAgent = false // To make sure agent does not get launched on this node.
	installer, err := install.New(ctx, install.RuntimeConfig{
		Config:         *config,
		Planner:        reconfigure.NewPlanner(config, state.Cluster),
		FSMFactory:     reconfigure.NewFSMFactory(*config),
		ClusterFactory: install.NewClusterFactory(*config),
		Engine:         engine,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return installer, nil
}
