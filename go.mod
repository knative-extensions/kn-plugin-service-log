module github.com/rhuss/kn-service-log

go 1.14

require (
	github.com/fatih/color v1.9.0
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.17.6
	k8s.io/apimachinery v0.17.6
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/klog v1.0.0
	knative.dev/client v0.15.1-0.20200622052426-607e366bc21d
)

replace (
	k8s.io/client-go => k8s.io/client-go v0.17.6

	// Holds my latest PR for inlining plugons
	knative.dev/client => github.com/rhuss/knative-client v0.0.0-20200622095205-21c5d9d02ef8
)
