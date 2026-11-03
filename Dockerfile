# syntax=docker/dockerfile:1
#
# Flagfall in one image: the bundled themes and admin panel are built with
# Node, embedded in a static Go binary, and run on a minimal non-root base.
#
#   docker build -t flagfall .
#   docker run -p 8080:8080 -v flagfall-data:/data flagfall

# ---- front ends (themes + admin panel) ----
# Every bundled theme (internal/themes/bundled) needs its package.json and
# dist/ copied here.
FROM node:22-alpine AS web
WORKDIR /src
RUN corepack enable
# Manifests first, so dependency installs are cached until they change.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/sdk/package.json web/sdk/
COPY web/admin/package.json web/admin/
COPY themes/amber-phosphor/package.json themes/amber-phosphor/
COPY themes/starter/package.json themes/starter/
COPY e2e/package.json e2e/
RUN pnpm install --frozen-lockfile \
      --filter "@flagfall/admin..." --filter "@flagfall/theme-*..."
COPY web/ web/
COPY themes/ themes/
RUN pnpm --filter "@flagfall/admin" --filter "@flagfall/theme-*" build

# ---- server binary ----
FROM golang:1.27-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/admin/dist/ web/admin/dist/
COPY --from=web /src/themes/amber-phosphor/dist/ themes/amber-phosphor/dist/
COPY --from=web /src/themes/starter/dist/ themes/starter/dist/
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/flagfall/flagfall/internal/version.Version=${VERSION} -X github.com/flagfall/flagfall/internal/version.Commit=${COMMIT}" \
      -o /out/flagfall ./cmd/flagfall \
 && mkdir -p /out/data

# ---- runtime ----
# distroless/static: CA certificates and tzdata are included (the binary
# also embeds tzdata); no shell or package manager.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/flagfall /usr/local/bin/flagfall
COPY --from=go --chown=nonroot:nonroot /out/data /data
ENV FLAGFALL_ADDR=":8080" \
    FLAGFALL_DATA_DIR="/data"
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
  CMD ["/usr/local/bin/flagfall", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/flagfall"]
CMD ["serve"]
