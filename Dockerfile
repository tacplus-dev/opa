# Uncle OPA Release Image
# Layers the uncleopa binary onto a minimal Alpine base.

FROM registry.gitlab.com/secondstarcavalier/schematic/containers/alpine/schematic-alpine-1-2:latest

ARG BUILD_DATE
ARG VCS_REF
ARG VERSION

LABEL org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.authors="Second Star Cavalier LLC <cloudctlplane@secondstarcavalier.com>" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.vendor="Second Star Cavalier LLC" \
      org.opencontainers.image.title="Uncle OPA" \
      org.opencontainers.image.description="OPA policy evaluation service for Cavalier control plane"

RUN addgroup -S uncleopa && adduser -S uncleopa -G uncleopa

COPY bin/uncleopa /usr/local/bin/uncleopa
RUN chmod +x /usr/local/bin/uncleopa

USER uncleopa

ENTRYPOINT ["uncleopa"]
