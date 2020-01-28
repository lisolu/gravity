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
	"strings"

	"github.com/gravitational/gravity/lib/fsm"
	"github.com/gravitational/gravity/lib/httplib"
	"github.com/gravitational/gravity/lib/install"
	installphases "github.com/gravitational/gravity/lib/install/phases"
	"github.com/gravitational/gravity/lib/install/reconfigure/phases"

	"github.com/gravitational/trace"
)

// FSMSpec returns a function that returns an appropriate phase executor
// based on the provided parameters.
func FSMSpec(config install.FSMConfig) fsm.FSMSpecFunc {
	return func(p fsm.ExecutorParams, remote fsm.Remote) (fsm.PhaseExecutor, error) {
		switch {
		case p.Phase.ID == installphases.ChecksPhase:
			return installphases.NewChecks(p,
				config.Operator,
				config.OperationKey)

		case p.Phase.ID == installphases.ConfigurePhase:
			return installphases.NewConfigure(p,
				config.Operator)

		case strings.HasPrefix(p.Phase.ID, installphases.PullPhase):
			return installphases.NewPull(p,
				config.Operator,
				config.Packages,
				config.LocalPackages,
				config.Apps,
				config.LocalApps,
				remote)

		// Reconfiguration is only currently supported for single-node clusters
		// so only "/masters" phase can be present.
		case strings.HasPrefix(p.Phase.ID, installphases.MastersPhase):
			return installphases.NewSystem(p,
				config.Operator,
				remote)

		case p.Phase.ID == installphases.WaitPhase:
			client, _, err := httplib.GetClusterKubeClient(p.Plan.DNSConfig.Addr())
			if err != nil {
				return nil, trace.Wrap(err)
			}
			return installphases.NewWait(p,
				config.Operator,
				client)

		case p.Phase.ID == installphases.HealthPhase:
			return installphases.NewHealth(p,
				config.Operator)

		case strings.HasPrefix(p.Phase.ID, phases.CleanupPhase):
			client, _, err := httplib.GetClusterKubeClient(p.Plan.DNSConfig.Addr())
			if err != nil {
				return nil, trace.Wrap(err)
			}
			return phases.NewCleanup(p,
				config.Operator,
				config.LocalPackages,
				client)

		default:
			return nil, trace.BadParameter("unknown phase %q", p.Phase.ID)
		}
	}
}
