# Opa

Opa is the centralized OPA (Open Policy Agent) policy evaluation service.
It executes typed OPA policy evaluations accessible to any service via S2S
gRPC. Policy definitions live in shared protos (`proto/opa/`) and are
importable by any service. Tacplus generates typed evaluation RPCs on Opa
and typed client wrappers on importing services' ServiceManagers.

The name is a play on the German translation of OPA (grandpa) — Uncle Grandpa.

## Architecture

Services that need policy decisions call Opa via generated S2S gRPC
clients. Opa evaluates OPA bundles against typed input messages and
returns typed evaluation results. Only kernel has direct access to Opa;
other services' policy evaluations are mediated through kernel.

## Configuration

| Field | Description |
|-------|-------------|
| `entitlements_policy` | OPA bundle for entitlement evaluation |

## Build

```bash
just        # generate + compile
```

## Proto

[`services/opa.proto`](../../proto/services/opa.proto)
