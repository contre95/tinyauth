package service

import (
	"slices"

	"github.com/tinyauthapp/tinyauth/internal/utils/logger"
	gateway "sigs.k8s.io/gateway-api/apis/v1"
)

type KubernetesHTTPRouteExtractor struct {
	log *logger.Logger
}

type KubernetesHTTPRouteExtractorInput struct {
	Log *logger.Logger
}

func NewKubernetesHTTPRouteExtractor(i KubernetesHTTPRouteExtractorInput) *KubernetesHTTPRouteExtractor {
	return &KubernetesHTTPRouteExtractor{
		log: i.Log,
	}
}

func (k *KubernetesHTTPRouteExtractor) getHosts(hostnames []gateway.Hostname) []string {
	var hosts []string

	for _, hostname := range hostnames {
		if hostname != "" {
			hosts = append(hosts, string(hostname))
		}
	}

	return hosts
}

func (k *KubernetesHTTPRouteExtractor) getRuleMatchers(matchers []gateway.HTTPRouteMatch) []string {
	var res []string

	for _, m := range matchers {
		if m.Path == nil {
			res = append(res, "/")
			continue
		}

		pathType := gateway.PathMatchPathPrefix
		if m.Path.Type != nil {
			pathType = *m.Path.Type
		}
		if pathType != gateway.PathMatchPathPrefix {
			continue
		}

		pathValue := "/"
		if m.Path.Value != nil {
			pathValue = *m.Path.Value
		}
		res = append(res, pathValue)
	}

	return res
}

func (k *KubernetesHTTPRouteExtractor) getPaths(rules []gateway.HTTPRouteRule) []string {
	var paths []string

	for _, rule := range rules {
		if len(rule.Matches) == 0 {
			paths = append(paths, "/")
			continue
		}
		matchers := k.getRuleMatchers(rule.Matches)
		paths = append(paths, matchers...)
	}

	return paths
}

func (k *KubernetesHTTPRouteExtractor) Extract(route *gateway.HTTPRoute) *ExtractionResult {
	hosts := k.getHosts(route.Spec.Hostnames)
	paths := k.getPaths(route.Spec.Rules)

	namespace := route.GetNamespace()
	name := route.GetName()

	annotations := route.GetAnnotations()

	if !slices.Contains(paths, "/") {
		k.log.App.Warn().Str("namespace", namespace).Str("name", name).Strs("paths", paths).Msg("Route does not contain a catch-all path, another route may be able to bypass auth checks if it routes the same host with a different path. Consider adding a catch-all path to this route to ensure auth checks are applied to all paths for this host.")
	}

	return &ExtractionResult{
		typ:         ResourceTypeHTTPRoute,
		name:        name,
		namespace:   namespace,
		hosts:       hosts,
		annotations: annotations,
	}
}
