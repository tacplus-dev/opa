# Provenance

Copied **wholesale** on 2026-10-23 from `gitlab.com/seabeam/schematic/platform/services/opa` at commit
`ba349a9` (local branch `d3-stratum-c2s-split`; its own nested git repository; history carried over), with
`proto/services/opa.proto` and `proto/dependencies.yaml` from `platform/proto` at `ca7c322` on branch `big8-p1-messaging`.
Build outputs not copied. 

Jody, 2026-10-23: *"We will copy kernel over to its github home and only make changes to it. we will
keep these as ships in the night for now. For the new stuff we want to do it the right way."* The private
copy runs the launch fleet unchanged; new-design changes land here. Stratum stays private.

Divergences: headers in tacplus.yaml (Jody Scott, Apache-2.0; LICENSE added); `src: proto` (this repo
holds the service's own proto); `proto/registry.yaml` is a lock of github.com/tacplus-dev/registry.

2026-11-06 (#3): the module is `github.com/tacplus-dev/opa` (was `gitlab.com/seabeam/schematic/platform/services/opa`),
its generated package `<module>/tacplus` (`tacplus.yaml` drops the copied `pkg`); SeaBeam's `.gitlab-ci.yml`
and the `registry:` block of `proto/dependencies.yaml` (its GitLab project id and interface names) are removed;
the image labels and the proto header name Jody Scott / tacplus.dev.
