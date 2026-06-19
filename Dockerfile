# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /panelgen .

# Runtime stage — scratch for minimum footprint
FROM scratch

COPY --from=builder /panelgen /panelgen

# panelgen needs filesystem access for:
#   - config file (panelgen.yml)
#   - style file (style.txt)
#   - reference images
#   - output directory
# Mount your working directory at /work and set it as the working dir.
WORKDIR /work

ENTRYPOINT ["/panelgen"]
