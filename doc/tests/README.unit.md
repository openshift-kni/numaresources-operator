Controllers and unit tests
==========================

Until 4.17 NROP was supported fully on one platform which is Openshift. Starting 4.18, support for Hypershift platform is added and is still at the constructive stages.
This affects the way the controllers behave on the different platforms and that is reflected in the controller tests which also must cover all supported versions.

This readme was written while the work for supporting Hypershift is on course and might need adjustments in a later stage when the work is settled for that platform.

The way the controller tests run for both versions in initializing the environment variable `TEST_PLATFORM` before running the tests.
The variable accepts two valid values:
* openshift
* hypershift

The make command for running the controllers tests is `make test-controllers` which is also called in the expanded target for alll the unit tests `make test-unit`.
The command will run a script to execute the controllers tests twice one per platform, thus results will also be reported for both platforms. 