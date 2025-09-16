/*
Copyright 2025.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ParkedDomainSpec defines the desired state of ParkedDomain.
type ParkedDomainSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// DomainName is the fully qualified domain name to park.
	DomainName string `json:"domainName"`
	Region     string `json:"region,omitempty"`
	// TemplateName is the name of the template file (e.g., "index.html")
	// to copy from the configmap.
	// +optional
	TemplateName string `json:"templateName,omitempty"`
}

// ParkedDomainStatus defines the observed state of ParkedDomain.
type ParkedDomainStatus struct {
	// Status indicates the current state, e.g., "Provisioned", "Error".
	Status string `json:"status,omitempty"`
	// ZoneID is the ID of the created Route 53 Hosted Zone.
	ZoneID string `json:"zoneID,omitempty"`
	// NameServers are the authoritative nameservers for the zone.
	NameServers []string `json:"nameServers,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ParkedDomain is the Schema for the parkeddomains API.
type ParkedDomain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ParkedDomainSpec   `json:"spec,omitempty"`
	Status ParkedDomainStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ParkedDomainList contains a list of ParkedDomain.
type ParkedDomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ParkedDomain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ParkedDomain{}, &ParkedDomainList{})
}
