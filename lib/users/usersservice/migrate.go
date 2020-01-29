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

package usersservice

import (
	"strings"

	"github.com/gravitational/gravity/lib/storage"

	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
)

// Migrate launches migrations for users and roles
func (u *UsersService) Migrate() error {
	users, err := u.backend.GetAllUsers()
	if err != nil {
		return trace.Wrap(err)
	}
	for _, user := range users {
		m := log.WithFields(log.Fields{"user": user.GetName(), "module": "migrate"})
		isAgent, err := u.isClusterAgent(user)
		if err != nil && !trace.IsNotFound(err) {
			return trace.Wrap(err)
		}
		if isAgent {
			m.Debugf("creating admin cluster user")
			err = u.insertAdminClusterAgent(user)
			if err != nil && !trace.IsAlreadyExists(err) {
				return trace.Wrap(err)
			}
		}
	}
	roles, err := u.backend.GetRoles()
	if err != nil {
		return trace.Wrap(err)
	}
	for _, irole := range roles {
		raw := irole.GetRawObject()
		if raw == nil {
			continue
		}
		role, ok := raw.(*storage.RoleV2)
		if ok {
			m := log.WithFields(log.Fields{"role": role.Metadata.Name, "module": "migrate"})
			m.Debugf("migrating V2 role")
			err := u.backend.UpsertRole(role.V3(), storage.Forever)
			if err != nil {
				return trace.Wrap(err)
			}
		}
	}
	return nil
}

// insertAdminClusterAgent inserts an admin cluster agent for the specified agent
func (u *UsersService) insertAdminClusterAgent(user storage.User) error {
	clusterName := user.GetClusterName()
	_, err := u.CreateClusterAdminAgent(
		clusterName, storage.NewUser(storage.ClusterAdminAgent(clusterName), storage.UserSpecV2{
			AccountID: user.GetAccountID(),
			OpsCenter: user.GetOpsCenter(),
		}))
	return trace.Wrap(err)
}

func (u *UsersService) isClusterAgent(user storage.User) (bool, error) {
	localCluster, err := u.GetLocalClusterName()
	if err != nil {
		if trace.IsNotFound(err) {
			return false, nil
		}
		return false, trace.Wrap(err)
	}
	return user.GetType() == storage.AgentUser &&
		strings.HasPrefix(user.GetName(), "agent") &&
		user.GetClusterName() == localCluster, nil
}
