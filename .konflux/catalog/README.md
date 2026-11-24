# FBC catalog

This branch builds the 4.23 FBC (`numaresources-operator-fbc-4-23`) from this directory.
The catalog contains the `stable` and `4.23` channels, starting with `numaresources-operator.v4.23.0`.
The first entry has no `replaces` field and allows upgrades from 4.22 through `skipRange`.

Konflux fills the bundle image placeholder in `catalog-template.in.yaml` from `bundle.builds.in.yaml`.
When a new 4.23 version is released, update the entries in both channels.

Catalogs are generated at Konflux build time and must not be committed.
Generated files (`catalog-template.out.yaml`, `numaresources-operator/catalog.json`) are listed in `.gitignore`.
