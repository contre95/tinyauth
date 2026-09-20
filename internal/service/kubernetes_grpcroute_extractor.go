package service

import (
	"github.com/tinyauthapp/tinyauth/internal/utils/logger"
	gateway "sigs.k8s.io/gateway-api/apis/v1"
)

type KubernetesGRPCRouteExtractor struct {
	log *logger.Logger
}

type KubernetesGRPCRouteExtractorInput struct {
	Log *logger.Logger
}

func NewKubernetesGRPCRouteExtractor(i KubernetesGRPCRouteExtractorInput) *KubernetesGRPCRouteExtractor {
	return &KubernetesGRPCRouteExtractor{
		log: i.Log,
	}
}

func (k *KubernetesGRPCRouteExtractor) getHosts(hostnames []gateway.Hostname) []string {
	var hosts []string

	for _, hostname := range hostnames {
		if hostname != "" {
			hosts = append(hosts, string(hostname))
		}
	}

	return hosts
}

func (k *KubernetesGRPCRouteExtractor) Extract(route *gateway.GRPCRoute) *ExtractionResult {
	hosts := k.getHosts(route.Spec.Hostnames)
	namespace := route.GetNamespace()
	name := route.GetName()
	annotations := route.GetAnnotations()

	return &ExtractionResult{
		typ:         ResourceTypeGRPCRoute,
		name:        name,
		namespace:   namespace,
		hosts:       hosts,
		annotations: annotations,
	}
}
