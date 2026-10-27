# Roadmap: opa

Owned by Jody; changed only by an agent at Jody's request. Loopers propose in PROPOSALS.md.

## Now
The policy evaluation service (Go): typed OPA evaluations over S2S gRPC, one eval RPC per `opa_eval` message in the fleet; only kernel calls it.
Unreleased; tacplus floor 2.4 declared on main (cca8282), gates waiting on the compiler image.

## Next
- Nothing unblocked: every open issue waits on #1, which waits on tacplus-dev/tacplus#189.

## Later
- Declare the tacplus floor 2.4: gates pass (#1; blocked on tacplus-dev/tacplus#189)
- Work without SeaBeam: no gitlab.com/seabeam module paths, sources or registries (#3; blocked on #1)
- 37 known vulnerabilities in grpc v1.79.3 and golang.org/x/{net,crypto,text,sys} (#4; blocked on #1)
- `gates` recipe in the justfile (#5; blocked on #1; a `gates` recipe is already on main since cca8282)
- Base image alpine:3.22 to 3.24 (#6; blocked on #5; main already defaults to alpine:3.24 since 9a8e516)
- Release: tag from v0.1.0 and publish the image (#2; blocked on #1, tacplus-dev/looper#12)

## Not doing
- Nothing recorded yet.
