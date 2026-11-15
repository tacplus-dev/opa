# Uncle OPA Release Image
# Layers the opa binary onto a minimal Alpine base.

# The base is a build argument: a fleet may hand in its own hardened base, the
# default is the public one (no private registry in a public contract's repo).
ARG BASE_IMAGE=alpine:3.24
FROM ${BASE_IMAGE}

ARG BUILD_DATE
ARG VCS_REF
ARG VERSION

LABEL org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.authors="Jody Scott <jody@thescottsweb.com>" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.vendor="tacplus.dev" \
      org.opencontainers.image.title="Uncle OPA" \
      org.opencontainers.image.description="OPA policy evaluation service for Schematic control plane"

RUN apk add --no-cache ca-certificates
COPY bin/opa /usr/local/bin/opa

# Every service runs under the launcher (github.com/tacplus-dev/launcher): PID 1
# that forwards signals, propagates the exit code, and kills a deadlocked child
# on gRPC health. The fleet's image pipeline puts the binary beside the Dockerfile.
ARG TARGETARCH
COPY launcher-linux-${TARGETARCH} /usr/local/bin/launcher
ENV LAUNCH_CMD="opa" HEALTH_PORT=8443

# Nothing opa does needs root, and it writes nothing (policies arrive over gRPC,
# the bootstrap config is read only). A fixed UID/GID, not an adduser default, so
# a pod's securityContext (runAsUser, fsGroup) can name it.
RUN addgroup -S -g 65532 opa && \
    adduser -S -D -H -u 65532 -G opa -s /sbin/nologin opa
USER 65532:65532
ENTRYPOINT ["launcher"]
