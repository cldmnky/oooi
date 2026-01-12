/*
Copyright 2026.

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

package envoy

import (
	"fmt"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

// BuildEnvoyBootstrapConfig generates the Envoy bootstrap configuration for xDS
func BuildEnvoyBootstrapConfig(proxy *hostedclusterv1alpha1.ProxyServer, xdsPort int32) string {
	return fmt.Sprintf(`{
  "node": {
    "id": "%s",
    "cluster": "%s"
  },
  "dynamic_resources": {
    "ads_config": {
      "api_type": "GRPC",
      "transport_api_version": "V3",
      "grpc_services": [
        {
          "envoy_grpc": {
            "cluster_name": "xds_cluster"
          }
        }
      ]
    },
    "cds_config": {
      "resource_api_version": "V3",
      "ads": {}
    },
    "lds_config": {
      "resource_api_version": "V3",
      "ads": {}
    }
  },
  "static_resources": {
    "clusters": [
      {
        "name": "xds_cluster",
        "connect_timeout": "5s",
        "type": "STATIC",
        "typed_extension_protocol_options": {
          "envoy.extensions.upstreams.http.v3.HttpProtocolOptions": {
            "@type": "type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions",
            "explicit_http_config": {
              "http2_protocol_options": {}
            }
          }
        },
        "load_assignment": {
          "cluster_name": "xds_cluster",
          "endpoints": [
            {
              "lb_endpoints": [
                {
                  "endpoint": {
                    "address": {
                      "socket_address": {
                        "address": "127.0.0.1",
                        "port_value": %d
                      }
                    }
                  }
                }
              ]
            }
          ]
        }
      }
    ]
  },
  "admin": {
    "address": {
      "socket_address": {
        "address": "0.0.0.0",
        "port_value": 9901
      }
    }
  },

}`, proxy.Name, proxy.Name, xdsPort)
}

// BuildListenerConfig builds the Listener configuration for SNI-based routing
func BuildListenerConfig(proxy *hostedclusterv1alpha1.ProxyServer) string {
	port := proxy.Spec.Port
	if port == 0 {
		port = 443
	}

	// Build filter chains for each backend with SNI matching
	return fmt.Sprintf(`{
  "@type": "type.googleapis.com/envoy.config.listener.v3.Listener",
  "name": "listener_%s",
  "address": {
    "socket_address": {
      "protocol": "TCP",
      "address": "0.0.0.0",
      "port_value": %d
    }
  }
}`, proxy.Name, port)
}
