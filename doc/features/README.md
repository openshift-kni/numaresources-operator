Features introspection in e2e tests
======================================

**Goal**

Provide a way to identify the active features for every supported operator version.

**Design**

- Provide the possibility to group a set of tests that verify a common feature using ginkgo Label("feature:<topic>").
  A Topic should describe the main feature being tested or the unique effect of the test on the cluster. For instance:
  `feature:schedrst`: indicates that the test is expected to restart the schedular, be it a pod restart or a complete removal of the scheduler and recreation.
  `feature:rtetols`: indicates that the test is testing the new feature RTE tolerations, which involves specific non-default configuration on the operator CRs hence functional effects.
  `feature:unsched`: indicates that the test workload is anticipated to be un-schedulable for any reason for example: insufficient resources (pod is pending), TAE (failed)..
  `feature:wlplacement`: indicates that the test challenges the scheduler to place a workload on a node. This is a common label for most of the functional tests.
    - Rules for creating a new topic
        - A topic should point out briefly the new feature being tested or the unique effect of the test on the cluster
        - The topic should be lowercase
        - For composite topics use `_` between the words, e.g no_nrt
        - Should not consist of any of `&|!,()/`
- Define the list of supported features for each version.
- Allow the user to list the supported features by adding new flag, `-inspect-features` which if passed the numaresources-operator binary on the controller pod will list the active features for the deployed version of the operator.
- For automated bugs' scenarios that their fix is not back-ported to all versions use keywords tags that briefly describes the feature that the bug fix addresses.

**List active features**

Once the operator is installed, perform the following on the controller manager pod:

```azure
oc exec -it numaresources-controller-manager-95dd55c6-k6428 -n openshift-numaresources -- /bin/numaresources-operator --inspect-features
 ```

Example output:
```
{"active":["config","nonreg","hostlevel","resacct","cache","stall","rmsched","rtetols","overhead","wlplacement","unsched","nonrt","taint","nodelabel","byres","tmpol"]}
```

**Use case example: run tests for supported features only**

To build the filter label query of the supported features on a specific version, a binary called `mkginkgolabelfilter` exists in `releases/support-tools` which expects a JSON input showing the supported features from the operator controller pod as follows:

To use the helper tool perform the following:

```
# curl -L -o mkginkgolabelfilter https://github.com/openshift-kni/numaresources-operator/releases/download/support-tools/mkginkgolabelfilter
# chmod 755 mkginkgolabelfilter
# ./mkginkgolabelfilter # This will wait to read the input which should be the output of the --inspect-features above
{"active":["config","nonreg","hostlevel","resacct","cache","stall","rmsched","rtetols","overhead","wlplacement","unsched","nonrt","taint","nodelabel","byres","tmpol"]}
feature: containsAny {config,nonreg,hostlevel,resacct,cache,stall,rmsched,rtetols,overhead,wlplacement,unsched,nonrt,taint,nodelabel,byres,tmpol}
```

Then later in the podman command of running the tests use `--filter-label` with the output of the tool to run tests of supported features only.  