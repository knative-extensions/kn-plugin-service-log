// Copyright © 2021 The Knative Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkg

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

func NewLogCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "kn service log NAME",
		Short: "Print the logs for a service",
		Long: `Print the logs for a service

Requires a connection to a Kubernetes cluster
`,
		RunE: printLogs,
	}

	// Initialize Kubernetes logging system
	klog.InitFlags(nil)
	flag.Parse()
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	AddNamespaceFlags(cmd.Flags(), false)
	return cmd
}

func printLogs(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return errors.New("service name required as argument")
	}
	// Take only one service for now
	service := args[0]

	// Connect to Kubernetes cluster
	clientConfig, ns, err := getClientConfig()
	if err != nil {
		return err
	}
	if namespace := cmd.Flag("namespace"); namespace != nil && namespace.Value.String() != "" {
		ns = namespace.Value.String()
	}

	// Client for accessing pods
	client, err := corev1.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch and log
	err = run(ctx, client.Pods(ns), service)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return nil
}

func run(ctx context.Context, client corev1.PodInterface, service string) error {
	added, removed, err := watchForPodEvents(ctx, client, service)
	if err != nil {
		return err
	}

	tails := make(map[string]*Tail)

	go func() {
		for p := range added {
			id := p.getID()
			if tails[id] != nil {
				continue
			}

			tail := NewTail(p.namespace, p.pod, p.podIndex, p.revision)
			tails[id] = tail

			tail.Start(ctx, client)
		}
	}()

	go func() {
		for p := range removed {
			id := p.getID()
			if tails[id] == nil {
				continue
			}
			tails[id].Close()
			delete(tails, id)
		}
	}()

	<-ctx.Done()

	return nil

}

// Monitored pods
type targetPod struct {
	namespace string
	pod       string
	podIndex  string
	revision  string
}

func (p targetPod) getID() string {
	return fmt.Sprintf("%s-%s-%s", p.namespace, p.pod, p.revision)
}

func watchForPodEvents(ctx context.Context, pod corev1.PodInterface, service string) (chan *targetPod, chan *targetPod,
	error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"serving.knative.dev/service": service,
		},
	}

	watcher, err := pod.Watch(context.TODO(), metav1.ListOptions{Watch: true, LabelSelector: labels.Set(labelSelector.MatchLabels).String()})
	if err != nil {
		return nil, nil, err
	}

	added := make(chan *targetPod)
	removed := make(chan *targetPod)

	go func() {
		for {
			select {
			case e := <-watcher.ResultChan():
				if e.Object == nil {
					// Closed because of error
					return
				}

				pod := e.Object.(*v1.Pod)
				revision := pod.Labels["serving.knative.dev/revision"]
				podName := pod.Name
				if len(pod.GenerateName) > 0 && len(pod.Name) > len(pod.GenerateName) {
					podName = podName[len(pod.GenerateName):]
				}
				switch e.Type {
				case watch.Added, watch.Modified:
					var statuses []v1.ContainerStatus
					statuses = append(statuses, pod.Status.InitContainerStatuses...)
					statuses = append(statuses, pod.Status.ContainerStatuses...)

					for _, c := range statuses {
						if c.Name != "user-container" {
							continue
						}
						if c.State.Running != nil {
							added <- &targetPod{
								namespace: pod.Namespace,
								pod:       pod.Name,
								podIndex:  podName,
								revision:  revision,
							}
						}
					}
				case watch.Deleted:
					var containers []v1.Container
					containers = append(containers, pod.Spec.Containers...)
					containers = append(containers, pod.Spec.InitContainers...)

					for _, c := range containers {
						if c.Name != "user-container" {
							continue
						}

						removed <- &targetPod{
							namespace: pod.Namespace,
							pod:       pod.Name,
							podIndex:  podName,
							revision:  revision,
						}
					}
				}
			case <-ctx.Done():
				watcher.Stop()
				close(added)
				close(removed)
				return
			}
		}
	}()

	return added, removed, nil
}

func getClientConfig() (*rest.Config, string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

	namespace, _, err := config.Namespace()
	if err != nil {
		return nil, "", fmt.Errorf("unable to get default namespace: %w", err)
	}
	client, err := config.ClientConfig()
	if err != nil {
		return nil, "", err
	}
	return client, namespace, nil
}
