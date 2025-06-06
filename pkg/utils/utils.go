/*
Copyright 2020 NVIDIA

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

// Package utils contains utils used across the code base.
package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/pkg/errors"

	"github.com/Mellanox/network-operator/pkg/clustertype"
	"github.com/Mellanox/network-operator/pkg/consts"
	"github.com/Mellanox/network-operator/pkg/staticconfig"
)

// GetFilesWithSuffix returns all files under a given base directory that have a specific suffix
// The operation is performed recursively on subdirectories as well.
func GetFilesWithSuffix(baseDir string, suffixes ...string) ([]string, error) {
	var files []string
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		// Error during traversal
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Skip non suffix files
		base := info.Name()
		for _, s := range suffixes {
			if strings.HasSuffix(base, s) {
				files = append(files, path)
			}
		}
		return nil
	})

	if err != nil {
		return nil, errors.Wrapf(err, "error traversing directory tree")
	}
	return files, nil
}

// GetNetworkAttachmentDefLink returns the location of the passed NetworkAttachmentDefinition Kubernetes resource.
func GetNetworkAttachmentDefLink(netAttDef *netattdefv1.NetworkAttachmentDefinition) (link string) {
	link = fmt.Sprintf("%s/namespaces/%s/%s/%s",
		netAttDef.APIVersion, netAttDef.Namespace, netAttDef.Kind, netAttDef.Name)
	return
}

// GetCniBinDirectory returns the location where the CNI binaries are stored on the node.
func GetCniBinDirectory(staticInfo staticconfig.Provider,
	clusterInfo clustertype.Provider) string {
	// First we try to set the user-set value, then fallback to defaults for Openshift / K8s
	userSetDirectory := staticInfo.GetStaticConfig().CniBinDirectory
	if userSetDirectory != "" {
		return userSetDirectory
	} else if clusterInfo != nil && clusterInfo.IsOpenshift() {
		// /opt/cni/bin directory is read-only on OCP, so we need to use another one
		return consts.OcpCniBinDirectory
	}
	return consts.DefaultCniBinDirectory
}

// GetCniNetworkDirectory returns the location where the CNI network configuration is stored on the node.
func GetCniNetworkDirectory(staticInfo staticconfig.Provider, _ clustertype.Provider) string {
	// First we try to set the user-set value, then fallback to defaults
	userSetDirectory := staticInfo.GetStaticConfig().CniNetworkDirectory
	if userSetDirectory != "" {
		return userSetDirectory
	}
	return consts.DefaultCniNetworkDirectory
}
