FROM node:22-bookworm-slim AS frontend
WORKDIR /build
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY src ./src
COPY docs ./docs
COPY tsconfig.json webpack.config.cjs README.md LICENSE ./
RUN npm run typecheck && npm run build

FROM golang:1.26-bookworm AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY pkg ./pkg
COPY cmd ./cmd
RUN CGO_ENABLED=1 go build -trimpath -o /plugin/gpx_local_duckdb_linux_$(go env GOARCH) ./pkg
RUN go run ./cmd/install-extensions /extensions

FROM grafana/grafana:12.4.0-ubuntu
USER root
COPY --from=frontend /build/dist /opt/grafana/plugins/local-duckdb-datasource
COPY --from=backend /plugin /opt/grafana/plugins/local-duckdb-datasource
COPY --from=backend /extensions /opt/duckdb/extensions
RUN mkdir -p /var/lib/ducklake/data && chown -R 472:0 /var/lib/ducklake /opt/grafana/plugins
ENV GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=local-duckdb-datasource
ENV GF_PATHS_PLUGINS=/opt/grafana/plugins
USER 472
