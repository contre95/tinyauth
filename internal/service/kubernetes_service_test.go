package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinyauthapp/tinyauth/internal/model"
	"github.com/tinyauthapp/tinyauth/internal/utils/logger"
	networking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	gateway "sigs.k8s.io/gateway-api/apis/v1"
)

func watchedResourceForTest(t *testing.T, typ ResourceType) watchedResource {
	t.Helper()
	for _, resource := range supportedResources {
		if resource.typ == typ {
			return resource
		}
	}
	t.Fatalf("unsupported resource type %q", typ)
	return watchedResource{}
}

func newKubernetesServiceForTest(log *logger.Logger) *KubernetesService {
	service := &KubernetesService{
		apps: make(map[resourceKey]routedApps),
		log:  log,
	}
	service.extractors.ingress = NewKubernetesIngressExtractor(KubernetesIngressExtractorInput{Log: log})
	service.extractors.httproute = NewKubernetesHTTPRouteExtractor(KubernetesHTTPRouteExtractorInput{Log: log})
	service.extractors.grpc = NewKubernetesGRPCRouteExtractor(KubernetesGRPCRouteExtractorInput{Log: log})
	return service
}

func testIngress(name string, annotations map[string]string, hosts ...string) *typedItem {
	rules := make([]networking.IngressRule, 0, len(hosts))
	for _, host := range hosts {
		rules = append(rules, networking.IngressRule{Host: host})
	}
	return &typedItem{
		typ: ResourceTypeIngress,
		ingress: &networking.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Annotations: annotations},
			Spec:       networking.IngressSpec{Rules: rules},
		},
	}
}

func testHTTPRoute(name string, annotations map[string]string, hosts ...string) *typedItem {
	hostnames := make([]gateway.Hostname, 0, len(hosts))
	for _, host := range hosts {
		hostnames = append(hostnames, gateway.Hostname(host))
	}
	return &typedItem{
		typ: ResourceTypeHTTPRoute,
		route: &gateway.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Annotations: annotations},
			Spec:       gateway.HTTPRouteSpec{Hostnames: hostnames, Rules: []gateway.HTTPRouteRule{{}}},
		},
	}
}

func testGRPCRoute(name string, annotations map[string]string, hosts ...string) *typedItem {
	hostnames := make([]gateway.Hostname, 0, len(hosts))
	for _, host := range hosts {
		hostnames = append(hostnames, gateway.Hostname(host))
	}
	return &typedItem{
		typ: ResourceTypeGRPCRoute,
		grpc: &gateway.GRPCRoute{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Annotations: annotations},
			Spec:       gateway.GRPCRouteSpec{Hostnames: hostnames},
		},
	}
}

func lookupApp(service *KubernetesService, domain string) *model.App {
	var app *model.App
	service.getEntry(domain, func(name string, candidate *model.App) bool {
		if candidate.Config.Domain == domain || strings.HasPrefix(domain, name+".") {
			app = candidate
			return true
		}
		return false
	})
	return app
}

func TestKubernetesServiceUpdateFromItem(t *testing.T) {
	log := logger.NewLogger().WithTestConfig()
	log.Init()

	tests := []struct {
		name             string
		resource         ResourceType
		item             *typedItem
		domain           string
		wantConfigDomain string
		allow            string
	}{
		{
			name:     "Ingress matches a configured domain",
			resource: ResourceTypeIngress,
			item: testIngress("ingress", map[string]string{
				"tinyauth.apps.dashboard.config.domain": "dashboard.example.com",
				"tinyauth.apps.dashboard.users.allow":   "alice",
			}, "dashboard.example.com"),
			domain: "dashboard.example.com", wantConfigDomain: "dashboard.example.com", allow: "alice",
		},
		{
			name:     "Ingress matches an app name case insensitively",
			resource: ResourceTypeIngress,
			item: testIngress("ingress", map[string]string{
				"tinyauth.apps.dashboard.users.allow": "alice",
			}, "Dashboard.example.com"),
			domain: "dashboard.example.com", allow: "alice",
		},
		{
			name:     "HTTPRoute matches a configured domain",
			resource: ResourceTypeHTTPRoute,
			item: testHTTPRoute("http-route", map[string]string{
				"tinyauth.apps.api.config.domain": "api.example.com",
				"tinyauth.apps.api.users.allow":   "bob",
			}, "api.example.com"),
			domain: "api.example.com", wantConfigDomain: "api.example.com", allow: "bob",
		},
		{
			name:     "HTTPRoute wildcard matches nested subdomains",
			resource: ResourceTypeHTTPRoute,
			item: testHTTPRoute("http-route", map[string]string{
				"tinyauth.apps.api.config.domain": "deep.api.example.com",
				"tinyauth.apps.api.users.allow":   "bob",
			}, "*.example.com"),
			domain: "deep.api.example.com", wantConfigDomain: "deep.api.example.com", allow: "bob",
		},
		{
			name:     "GRPCRoute matches a configured domain",
			resource: ResourceTypeGRPCRoute,
			item: testGRPCRoute("grpc-route", map[string]string{
				"tinyauth.apps.grpc.config.domain": "grpc.example.com",
				"tinyauth.apps.grpc.users.allow":   "carol",
			}, "grpc.example.com"),
			domain: "grpc.example.com", wantConfigDomain: "grpc.example.com", allow: "carol",
		},
		{
			name:     "GRPCRoute matches an app name through a wildcard",
			resource: ResourceTypeGRPCRoute,
			item: testGRPCRoute("grpc-route", map[string]string{
				"tinyauth.apps.grpc.users.allow": "carol",
			}, "*.example.com"),
			domain: "grpc.example.com", allow: "carol",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newKubernetesServiceForTest(log)
			service.updateFromItem(watchedResourceForTest(t, test.resource), test.item)

			app := lookupApp(service, test.domain)
			require.NotNil(t, app)
			assert.Equal(t, test.allow, app.Users.Allow)
			assert.Equal(t, test.wantConfigDomain, app.Config.Domain)
		})
	}
}

func TestKubernetesServiceUpdateFromItemRemovesStaleEntries(t *testing.T) {
	log := logger.NewLogger().WithTestConfig()
	log.Init()

	tests := []struct {
		name     string
		resource ResourceType
		item     *typedItem
	}{
		{"Ingress without annotations", ResourceTypeIngress, testIngress("route", nil, "app.example.com")},
		{"Ingress without hosts", ResourceTypeIngress, testIngress("route", map[string]string{"tinyauth.apps.app.users.allow": "alice"})},
		{"HTTPRoute without annotations", ResourceTypeHTTPRoute, testHTTPRoute("route", nil, "app.example.com")},
		{"HTTPRoute without hosts", ResourceTypeHTTPRoute, testHTTPRoute("route", map[string]string{"tinyauth.apps.app.users.allow": "alice"})},
		{"GRPCRoute without annotations", ResourceTypeGRPCRoute, testGRPCRoute("route", nil, "app.example.com")},
		{"GRPCRoute without hosts", ResourceTypeGRPCRoute, testGRPCRoute("route", map[string]string{"tinyauth.apps.app.users.allow": "alice"})},
		{"Ingress with invalid annotations", ResourceTypeIngress, testIngress("route", map[string]string{"tinyauth.apps.app.users.break": "invalid"}, "app.example.com")},
		{"HTTPRoute with invalid annotations", ResourceTypeHTTPRoute, testHTTPRoute("route", map[string]string{"tinyauth.apps.app.users.break": "invalid"}, "app.example.com")},
		{"GRPCRoute with invalid annotations", ResourceTypeGRPCRoute, testGRPCRoute("route", map[string]string{"tinyauth.apps.app.users.break": "invalid"}, "app.example.com")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newKubernetesServiceForTest(log)
			key := resourceKey{typ: test.resource, namespace: "default", name: "route"}
			service.addResourceEntries(key, []string{"app.example.com"}, []resourceEntry{{
				name: "app",
				app:  model.App{Config: model.AppConfig{Domain: "app.example.com"}},
			}})

			service.updateFromItem(watchedResourceForTest(t, test.resource), test.item)
			assert.Nil(t, lookupApp(service, "app.example.com"))
		})
	}
}

func TestTypedItemFromUnstructured(t *testing.T) {
	tests := []struct {
		name     string
		resource ResourceType
		item     unstructured.Unstructured
		assert   func(t *testing.T, item *typedItem)
	}{
		{
			name:     "Ingress",
			resource: ResourceTypeIngress,
			item: unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"name": "ingress", "namespace": "default"},
				"spec":     map[string]any{"rules": []any{map[string]any{"host": "app.example.com"}}},
			}},
			assert: func(t *testing.T, item *typedItem) {
				require.NotNil(t, item.ingress)
				assert.Equal(t, "app.example.com", item.ingress.Spec.Rules[0].Host)
			},
		},
		{
			name:     "HTTPRoute",
			resource: ResourceTypeHTTPRoute,
			item: unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"name": "http-route", "namespace": "default"},
				"spec":     map[string]any{"hostnames": []any{"app.example.com"}},
			}},
			assert: func(t *testing.T, item *typedItem) {
				require.NotNil(t, item.route)
				assert.Equal(t, gateway.Hostname("app.example.com"), item.route.Spec.Hostnames[0])
			},
		},
		{
			name:     "GRPCRoute",
			resource: ResourceTypeGRPCRoute,
			item: unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"name": "grpc-route", "namespace": "default"},
				"spec":     map[string]any{"hostnames": []any{"app.example.com"}},
			}},
			assert: func(t *testing.T, item *typedItem) {
				require.NotNil(t, item.grpc)
				assert.Equal(t, gateway.Hostname("app.example.com"), item.grpc.Spec.Hostnames[0])
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item, err := new(typedItem).fromUnstructured(test.resource, &test.item)
			require.NoError(t, err)
			assert.Equal(t, test.resource, item.typ)
			test.assert(t, item)
		})
	}
}

func TestKubernetesServiceLookup(t *testing.T) {
	log := logger.NewLogger().WithTestConfig()
	log.Init()

	tests := []struct {
		name      string
		connected bool
		domain    string
		wantApp   bool
	}{
		{"Returns a matching app when connected", true, "app.example.com", true},
		{"Skips the cache before the service is connected", false, "app.example.com", false},
		{"Skips an invalid domain", true, "app.example.com\xC3\xA9", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newKubernetesServiceForTest(log)
			service.connected = test.connected
			service.addResourceEntries(resourceKey{typ: ResourceTypeIngress, namespace: "default", name: "route"}, []string{"app.example.com"}, []resourceEntry{{
				name: "app",
				app:  model.App{Config: model.AppConfig{Domain: "app.example.com"}},
			}})

			var app *model.App
			err := service.Lookup(test.domain, func(_ string, candidate *model.App) bool {
				app = candidate
				return true
			})
			require.NoError(t, err)
			assert.Equal(t, test.wantApp, app != nil)
		})
	}
}

func TestKubernetesServiceKeepsResourceTypesSeparate(t *testing.T) {
	log := logger.NewLogger().WithTestConfig()
	log.Init()
	service := newKubernetesServiceForTest(log)

	resources := []struct {
		resource ResourceType
		item     *typedItem
		domain   string
	}{
		{ResourceTypeIngress, testIngress("shared", map[string]string{"tinyauth.apps.ingress.config.domain": "ingress.example.com"}, "ingress.example.com"), "ingress.example.com"},
		{ResourceTypeHTTPRoute, testHTTPRoute("shared", map[string]string{"tinyauth.apps.http.config.domain": "http.example.com"}, "http.example.com"), "http.example.com"},
		{ResourceTypeGRPCRoute, testGRPCRoute("shared", map[string]string{"tinyauth.apps.grpc.config.domain": "grpc.example.com"}, "grpc.example.com"), "grpc.example.com"},
	}

	for _, resource := range resources {
		service.updateFromItem(watchedResourceForTest(t, resource.resource), resource.item)
	}
	for _, resource := range resources {
		assert.NotNil(t, lookupApp(service, resource.domain))
	}
}

func TestKubernetesHTTPRouteExtractorPaths(t *testing.T) {
	log := logger.NewLogger().WithTestConfig()
	log.Init()
	extractor := NewKubernetesHTTPRouteExtractor(KubernetesHTTPRouteExtractorInput{Log: log})

	prefix := gateway.PathMatchPathPrefix
	exact := gateway.PathMatchExact
	api := "/api"

	tests := []struct {
		name  string
		rules []gateway.HTTPRouteRule
		want  []string
	}{
		{"Rule without matches defaults to catch-all", []gateway.HTTPRouteRule{{}}, []string{"/"}},
		{"Match without path defaults to catch-all", []gateway.HTTPRouteRule{{Matches: []gateway.HTTPRouteMatch{{}}}}, []string{"/"}},
		{"Path defaults apply independently", []gateway.HTTPRouteRule{{Matches: []gateway.HTTPRouteMatch{{Path: &gateway.HTTPPathMatch{}}}}}, []string{"/"}},
		{"Exact paths do not count as catch-all", []gateway.HTTPRouteRule{{Matches: []gateway.HTTPRouteMatch{{Path: &gateway.HTTPPathMatch{Type: &exact}}}}}, nil},
		{"Prefix paths are retained", []gateway.HTTPRouteRule{{Matches: []gateway.HTTPRouteMatch{{Path: &gateway.HTTPPathMatch{Type: &prefix, Value: &api}}}}}, []string{"/api"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, extractor.getPaths(test.rules))
		})
	}
}

func TestKubernetesHostMatching(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		domain string
		want   bool
	}{
		{"Exact host", "app.example.com", "app.example.com", true},
		{"Case insensitive exact host", "App.Example.com", "app.example.com", true},
		{"Wildcard host", "*.example.com", "deep.app.example.com", true},
		{"Wildcard does not match its apex", "*.example.com", "example.com", false},
		{"Different host", "app.example.com", "other.example.com", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, hostMatchesHostname(test.host, test.domain))
		})
	}
}
