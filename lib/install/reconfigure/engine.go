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

package reconfigure

import (
	"context"
	"time"

	"github.com/gravitational/gravity/lib/defaults"
	"github.com/gravitational/gravity/lib/install"
	"github.com/gravitational/gravity/lib/install/dispatcher"
	"github.com/gravitational/gravity/lib/ops"
	"github.com/gravitational/gravity/lib/schema"
	"github.com/gravitational/gravity/lib/storage"
	"github.com/gravitational/gravity/lib/systeminfo"

	"github.com/gravitational/trace"
	"github.com/sirupsen/logrus"
)

// NewEngine returns fsm engine for the reconfigure operation.
func NewEngine(config Config) (*Engine, error) {
	if err := config.checkAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}
	return &Engine{
		Config: config,
	}, nil
}

// Engine implements command line-driven installation workflow
type Engine struct {
	// Config specifies the engine's configuration
	Config
}

// Config is the reconfigure operation engine configuration.
type Config struct {
	// FieldLogger is the logger for the installer
	logrus.FieldLogger
	// Operator specifies the service operator
	ops.Operator
	// InstallOperation is the original install operation of this cluster.
	InstallOperation *storage.SiteOperation
}

func (c *Config) checkAndSetDefaults() error {
	if c.FieldLogger == nil {
		c.FieldLogger = logrus.WithField(trace.Component, "engine:reconfigure")
	}
	if c.Operator == nil {
		return trace.BadParameter("missing Operator")
	}
	return nil
}

// Execute executes the installer steps.
// Implements installer.Engine
func (e *Engine) Execute(ctx context.Context, installer install.Interface, config install.Config) (dispatcher.Status, error) {
	err := e.execute(ctx, installer, config)
	if err != nil {
		return dispatcher.StatusUnknown, trace.Wrap(err)
	}
	return dispatcher.StatusCompleted, nil
}

func (e *Engine) execute(ctx context.Context, installer install.Interface, config install.Config) (err error) {
	operation, err := e.upsertClusterAndOperation(ctx, installer, config)
	if err != nil {
		return trace.Wrap(err, "failed to create cluster/operation")
	}
	if err := installer.ExecuteOperation(operation.Key()); err != nil {
		return trace.Wrap(err)
	}
	if err := installer.CompleteOperation(*operation); err != nil {
		e.WithError(err).Warn("Failed to finalize the operation.")
	}
	return nil
}

func (e *Engine) upsertClusterAndOperation(ctx context.Context, installer install.Interface, config install.Config) (*ops.SiteOperation, error) {
	clusters, err := e.Operator.GetSites(defaults.SystemAccountID)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	var cluster *ops.Site
	if len(clusters) == 0 {
		cluster, err = e.Operator.CreateSite(installer.NewCluster())
		if err != nil {
			return nil, trace.Wrap(err)
		}
	} else {
		cluster = &clusters[0]
	}
	operations, err := e.Operator.GetSiteOperations(cluster.Key())
	if err != nil {
		return nil, trace.Wrap(err)
	}
	var operation *ops.SiteOperation
	if len(operations) == 0 {
		operation, err = e.createOperation(ctx, config)
		if err != nil {
			return nil, trace.Wrap(err)
		}
	} else {
		operation = (*ops.SiteOperation)(&operations[0])
	}
	return operation, nil
}

func (e *Engine) createOperation(ctx context.Context, config install.Config) (*ops.SiteOperation, error) {
	systemInfo, err := systeminfo.New()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	server := storage.Server{
		AdvertiseIP: config.AdvertiseAddr,
		Hostname:    systemInfo.GetHostname(),
		// Nodename: ,
		Role: config.Role,
		// InstanceType: ,
		// InstanceID: ,
		ClusterRole: string(schema.ServiceRoleMaster),
		Provisioner: schema.ProvisionerOnPrem,
		OSInfo:      systemInfo.GetOS(),
		// Mounts: ,
		// SystemState: ,
		// Docker: ,
		User:    systemInfo.GetUser(),
		Created: time.Now().UTC(),
	}
	req := ops.CreateClusterReconfigureOperationRequest{
		SiteKey: ops.SiteKey{
			AccountID:  defaults.SystemAccountID,
			SiteDomain: config.SiteDomain,
		},
		AdvertiseAddr: config.AdvertiseAddr,
		Token:         config.Token.Token,
		Servers:       []storage.Server{server},
		InstallExpand: e.InstallOperation.InstallExpand,
	}
	key, err := e.Operator.CreateClusterReconfigureOperation(ctx, req)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	operation, err := e.Operator.GetSiteOperation(*key)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return operation, nil
}
