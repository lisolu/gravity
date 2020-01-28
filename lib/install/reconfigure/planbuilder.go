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
	"fmt"

	"github.com/gravitational/gravity/lib/fsm"
	"github.com/gravitational/gravity/lib/install"
	installphases "github.com/gravitational/gravity/lib/install/phases"
	"github.com/gravitational/gravity/lib/install/reconfigure/phases"
	"github.com/gravitational/gravity/lib/storage"
)

// PlanBuilder builds plan for the reconfigure operation.
type PlanBuilder struct {
	*install.PlanBuilder
}

// AddCleanupPhase adds the post IP change cleanup phases to the plan.
func (b *PlanBuilder) AddCleanupPhase(plan *storage.OperationPlan) {
	plan.Phases = append(plan.Phases, storage.OperationPhase{
		ID:          phases.CleanupPhase,
		Description: "Repair the cluster state after advertise IP change",
		Requires:    fsm.RequireIfPresent(plan, installphases.HealthPhase),
		Phases: []storage.OperationPhase{
			{
				ID:          fmt.Sprintf("%v/%v", phases.CleanupPhase, phases.PackagesPhase),
				Description: "Remove old configuration and secrets",
				Data: &storage.OperationPhaseData{
					Server: &b.Master,
				},
			},
			{
				ID:          fmt.Sprintf("%v/%v", phases.CleanupPhase, phases.StatePhase),
				Description: "Update cluster state",
				Data: &storage.OperationPhaseData{
					Server: &b.Master,
				},
			},
			{
				ID:          fmt.Sprintf("%v/%v", phases.CleanupPhase, phases.NetworkPhase),
				Description: "Remove old network interfaces",
				Data: &storage.OperationPhaseData{
					Server: &b.Master,
				},
			},
			{
				ID:          fmt.Sprintf("%v/%v", phases.CleanupPhase, phases.TokensPhase),
				Description: "Remove old service account tokens",
				Data: &storage.OperationPhaseData{
					Server: &b.Master,
				},
			},
			{
				ID:          fmt.Sprintf("%v/%v", phases.CleanupPhase, phases.NodePhase),
				Description: "Remove old Kubernetes node",
				Data: &storage.OperationPhaseData{
					Server: &b.Master,
				},
			},
		},
	})
}
