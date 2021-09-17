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
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"text/template"

	"github.com/fatih/color"
	v12 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"k8s.io/client-go/rest"
)

type Tail struct {
	namespace     string
	podName       string
	revisionName  string
	req           *rest.Request
	closed        chan struct{}
	revisionColor *color.Color
	podColor      *color.Color
	tmpl          *template.Template
	podIndex      string
}

var colorList = []*color.Color{
	color.New(color.FgHiCyan),
	color.New(color.FgHiGreen),
	color.New(color.FgHiMagenta),
	color.New(color.FgHiYellow),
	color.New(color.FgHiBlue),
	color.New(color.FgHiRed),
}

var defaultTemplate = "{{color .RevisionColor .RevisionName}} {{color .PodColor .PodIndex}} {{.Message}}"

func NewTail(namespace, podName, podIndex, revisionName string) *Tail {
	podColor := randomColor(podName)
	revisionColor := randomColor(revisionName)

	funs := map[string]interface{}{
		"color": func(color color.Color, text string) string {
			return color.SprintFunc()(text)
		},
	}
	tmpl := template.Must(template.New("log").Funcs(funs).Parse(defaultTemplate))

	return &Tail{
		namespace:     namespace,
		podName:       podName,
		podIndex:      podIndex,
		revisionName:  revisionName,
		tmpl:          tmpl,
		podColor:      podColor,
		revisionColor: revisionColor,
		closed:        make(chan struct{}),
	}
}

func (t *Tail) Start(ctx context.Context, pod v1.PodInterface) {
	go func() {
		g := color.New(color.FgHiGreen, color.Bold).SprintFunc()
		t.printMarker(g("+"))

		req := pod.GetLogs(t.podName, &v12.PodLogOptions{
			Follow:     true,
			Timestamps: true,
			Container:  "user-container",
		})

		stream, err := req.Stream(context.TODO())
		if err != nil {
			fmt.Println(fmt.Errorf("Error opening stream to for revision %s: (%s/%s) : %v\n", t.revisionName, t.namespace, t.podName, err))
			return
		}
		defer stream.Close()

		go func() {
			<-t.closed
			stream.Close()
		}()

		reader := bufio.NewReader(stream)

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}

			str := string(line)
			t.print(str)
		}
	}()

	go func() {
		<-ctx.Done()
		close(t.closed)
	}()
}

// close stops tailing
func (t *Tail) Close() {
	d := color.New(color.FgHiRed, color.Bold).SprintFunc()
	t.printMarker(d("-"))
	close(t.closed)
}

// print prints a color coded log message with the pod and container names
func (t *Tail) print(msg string) {
	vm := Log{
		Message:       msg,
		Namespace:     t.namespace,
		PodName:       t.podName,
		PodIndex:      t.podIndex,
		RevisionName:  t.revisionName,
		PodColor:      t.podColor,
		RevisionColor: t.revisionColor,
	}
	err := t.tmpl.Execute(os.Stdout, vm)
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("expanding template failed: %s", err))
	}
}

func (t *Tail) printMarker(prefix string) {
	r := t.revisionColor.SprintFunc()
	p := t.podColor.SprintFunc()
	fmt.Fprintf(os.Stderr, "%s %s %s\n", prefix, r(t.revisionName), p(t.podIndex))
}

func randomColor(revisionName string) *color.Color {
	hash := fnv.New32()
	hash.Write([]byte(revisionName))
	idx := hash.Sum32() % uint32(len(colorList))
	return colorList[idx]
}

// Log is the object which will be used together with the template to generate
// the output.
type Log struct {
	// Message is the log message itself
	Message string

	// namespace of the pod
	Namespace string

	// podName of the pod
	PodName string

	// PodIndex is the pod's short name
	PodIndex string

	// revisionName of the container
	RevisionName string

	// PodColor is the color for printing a pod
	PodColor *color.Color

	// Revision color to use
	RevisionColor *color.Color
}
