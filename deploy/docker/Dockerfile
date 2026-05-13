# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/odin ./cmd/odin \
  && mkdir -p /out/odin-root /out/tmp

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/odin /usr/local/bin/odin
COPY --from=build --chown=nonroot:nonroot /out/odin-root /var/lib/odin-os
COPY --from=build --chown=nonroot:nonroot /out/tmp /tmp
COPY --chown=nonroot:nonroot config ./config
COPY --chown=nonroot:nonroot registry ./registry
COPY --chown=nonroot:nonroot prompts ./prompts

ENV ODIN_ROOT=/var/lib/odin-os
ENV ODIN_HTTP_ADDR=0.0.0.0:9444

USER nonroot:nonroot
EXPOSE 9444

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/usr/local/bin/odin", "healthcheck"]

CMD ["/usr/local/bin/odin", "serve"]
