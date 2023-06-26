About collecting numa-aware scheduling related data
===================================================

You can use the `oc adm must-gather` CLI command to collect information about your cluster, including features and objects associated
with the areas managed by numaresources-operator, and pertaining to the numa-aware scheduler.

To collect the numa-aware scheduler and numaresources operator data with must-gather, you must specify the extra image using the `--image` option.
In the following examples, `TAG` has the format `major.minor-snapshot`. For example, the TAG for OpenShift 4.12 will be `4.12-snapshot`.

Example command line:
```bash
oc adm must-gather --image=quay.io/openshift-kni/numaresources-must-gather:$TAG
```

To collect the cluster-related data and *additionally* the performance-related data, you can use multiple `--image` options like in this example:
```bash
oc adm must-gather --image=quay.io/openshift/origin-must-gather --image=quay.io/openshift-kni/numaresources-must-gather:$TAG
```

