package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// kubeTarget is a Kubernetes object named on the command line as
// k8s://RESOURCE, such as k8s://deploy/api?namespace=prod, edited the
// way :kedit does.
type kubeTarget struct {
	resource string

	// opts are the kubectl options the query named.
	opts []string
}

// kubeQueryFlags maps the query keys of a k8s:// URL to kubectl flags,
// ns is the short form of namespace.
var kubeQueryFlags = map[string]string{"ns": "-n", "namespace": "-n", "context": "--context", "kubeconfig": "--kubeconfig"}

// parseKube reads a k8s:// URL. The host and the path form the
// resource, the query names the namespace (ns for short), context and
// kubeconfig.
func parseKube(arg string) (kubeTarget, error) {
	u, err := url.Parse(arg)
	if err != nil || u.Scheme != "k8s" {
		return kubeTarget{}, fmt.Errorf("%s is not a k8s:// URL", arg)
	}

	resource := strings.Trim(u.Host+u.Path, "/")
	if resource == "" {
		return kubeTarget{}, errors.New("k8s:// needs a resource such as k8s://deploy/api")
	}

	t := kubeTarget{resource: resource}
	for key, values := range u.Query() {
		flag, ok := kubeQueryFlags[key]
		if !ok {
			return kubeTarget{}, fmt.Errorf("k8s:// takes ns, context and kubeconfig, not %s", key)
		}

		t.opts = append(t.opts, flag, values[0])
	}

	return t, nil
}

// editKube starts a session on the object of t.
func editKube(t kubeTarget, lang string) error {
	start := sessionStart{doc: editor.NewDocument("", ""), lang: lang, kube: &t}

	return session(host.Local{}, "", config.WorkspaceDir(), start)
}
