# PFP status alignment verification tool


`pfpsyncchk` is a tool that checks if provided pod-fingerprint (PFP) statuses from `resources-topology-exporter` (RTE) and `secondary-scheduler` (SS) pods are in sync or not. If the reported statuses are not matching, the scheduler will be hitting an `invalid topology data` case which indicates that the node topology data on the scheduler end is out of date. The scheduler should eventually recover from this state but if it doesn't it points out an alignment problem between RTE and SS with respect to how each one is computing the PFP from its end. 



The tool accepts two options to perform the check:
1. `pfpsyncchk -must-gather <must-gather-directory>`

After collecting numaresources-operator must-gather, unpack the must-gather tar and provide the directory trusted path to `-must-gather` flag. The tool will look for the PFP statuses files and apply the check per node.

2. `pfpsyncchk -from-rte <file-1-path> -from-scheduler <file-2-path>`

This option runs the verification functionality on two files assuming they belong to the same node but one is collected via RTE pod and the other collected from SS for the desired node.  


**Example**

```
$ bin/pfpsyncchk -must-gather tools/pfpsyncchk/testdata/valid-must-gather
```

output:

```
I1001 13:11:27.787313 2663443 pfpsyncchk.go:182] "scheduler is off-sync with RTE" podfingerprint="pfp0v001dd93850006612179"
I1001 13:11:27.787397 2663443 pfpsyncchk.go:182] "scheduler is off-sync with RTE" podfingerprint="pfp0v0019316f7e44a76ff27"
I1001 13:11:27.787918 2663443 pfpsyncchk.go:182] "scheduler is off-sync with RTE" podfingerprint="pfp0v0017dd331cc313ac040"
******************************************************************************************
Processing node:  control-plane-1
******************************************************************************************
Expected PFP: pfp0v001dd93850006612179
Computed PFP: pfp0v0012a06b0bbc2e0ed86
Found on RTE: true
Last write from scheduler: 2025-09-29 06:19:38.499706207 +0000 UTC
Last write from RTE: 2025-09-29 06:19:29.687691808 +0000 UTC

Pods found on RTE only:
Pods found on scheduler only:
 - openshift-kube-apiserver/installer-9-control-plane-1
 - openshift-kube-scheduler/revision-pruner-6-control-plane-1
 - numaresources/test-dp
 - openshift-kube-apiserver/installer-12-control-plane-1
 - openshift-kube-apiserver/installer-11-control-plane-1
 - openshift-etcd/revision-pruner-10-control-plane-1
 - openshift-kube-apiserver/installer-10-control-plane-1
 - openshift-kube-apiserver/installer-7-control-plane-1

________________________________
Expected PFP: pfp0v0019316f7e44a76ff27
Computed PFP: pfp0v0012a06b0bbc2e0ed86
Found on RTE: true
Last write from scheduler: 2025-09-29 06:20:23.606545132 +0000 UTC
Last write from RTE: 2025-09-29 06:20:19.687254798 +0000 UTC

Pods found on RTE only:
Pods found on scheduler only:
 - openshift-kube-apiserver/installer-12-control-plane-1
 - openshift-kube-apiserver/installer-11-control-plane-1
 - openshift-kube-apiserver/installer-7-control-plane-1
 - openshift-kube-apiserver/installer-9-control-plane-1
 - openshift-etcd/revision-pruner-10-control-plane-1
 - openshift-kube-scheduler/revision-pruner-6-control-plane-1
 - openshift-kube-apiserver/installer-10-control-plane-1

________________________________

******************************************************************************************
Processing node:  control-plane-2
******************************************************************************************
Expected PFP: pfp0v0017dd331cc313ac040
Computed PFP: pfp0v001f00d117476e39aba
Found on RTE: true
Last write from scheduler: 2025-09-29 06:20:23.602963713 +0000 UTC
Last write from RTE: 2025-09-29 06:20:12.744244581 +0000 UTC

Pods found on RTE only:
Pods found on scheduler only:
 - openshift-kube-apiserver/installer-12-control-plane-2
 - openshift-kube-apiserver/installer-7-control-plane-2
 - openshift-kube-apiserver/installer-10-control-plane-2
 - openshift-kube-scheduler/revision-pruner-6-control-plane-2
 - openshift-kube-apiserver/installer-11-control-plane-2
 - openshift-kube-apiserver/installer-9-control-plane-2
 - openshift-etcd/revision-pruner-10-control-plane-2

________________________________

******************************************************************************************
Processing node:  control-plane-3
******************************************************************************************
Expected PFP: pfp0v001e5569d55891c3a90
Computed PFP: pfp0v0016bebb9b36718de54
Found on RTE: true
Last write from scheduler: 2025-09-29 06:20:23.604753254 +0000 UTC
Last write from RTE: 2025-09-29 06:20:22.465965535 +0000 UTC

Pods found on RTE only:
Pods found on scheduler only:
 - openshift-kube-apiserver/installer-10-control-plane-3
 - openshift-kube-apiserver/installer-11-control-plane-3
 - openshift-kube-scheduler/revision-pruner-6-control-plane-3
 - openshift-etcd/revision-pruner-10-control-plane-3
 - numaresources/hello-completed-norestart
 - openshift-kube-apiserver/installer-12-control-plane-3
 - openshift-kube-apiserver/installer-7-control-plane-3
 - openshift-kube-apiserver/installer-9-control-plane-3

________________________________


```

**must-gather 


The executable is shipped as part of the must-gather image. Running the tool can be done using podman (or docker) following the below command:

```
podman run --rm -v <must-gather-directory>:/mgdata:Z --entrypoint /usr/bin/pfpsyncchk <must-gather-image> -must-gather /mgdata
```

Note: `:Z` flag tells podman to relabel the volume mount for SELinux compatibility so that it allows the container to read the mounted directory files.