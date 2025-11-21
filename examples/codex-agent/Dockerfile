ARG NODE_IMAGE_VERSION=24-bookworm
FROM node:${NODE_IMAGE_VERSION}

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ARG CODEX_IMAGE_VERSION
ENV CODEX_IMAGE_VERSION=${CODEX_IMAGE_VERSION}

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=UTC \
    GOPATH=/root/go \
    PATH=/usr/local/go/bin:/root/go/bin:/usr/local/bin:/usr/bin:/bin

RUN apt-get update -y \
  && apt-get install -y --no-install-recommends \
      ca-certificates curl wget gnupg lsb-release \
      git jq python3 python3-pip python3-venv \
      postgresql-client redis-tools amqp-tools \
      netcat-openbsd unzip zip make build-essential \
      gettext-base \
      software-properties-common ripgrep \
  && rm -rf /var/lib/apt/lists/*

# gh (GitHub CLI)
RUN install -m 0755 -d /etc/apt/keyrings \
 && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
 && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
      | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
 && apt-get update -y \
 && apt-get install -y gh \
 && rm -rf /var/lib/apt/lists/*

# Codex CLI
RUN npm install -g @openai/codex \
 && ln -sf "$(npm bin -g)/codex" /usr/local/bin/codex || true

# Go 1.25.1
RUN curl -fsSL https://go.dev/dl/go1.25.1.linux-amd64.tar.gz -o /tmp/go.tgz \
 && rm -rf /usr/local/go \
 && tar -C /usr/local -xzf /tmp/go.tgz \
 && rm /tmp/go.tgz

# Protoc (Protocol Buffers compiler)
RUN set -euo pipefail \
 && PROTOC_VERSION="${PROTOC_VERSION:-32.1}" \
 && TMP_PROTOC_DIR="$(mktemp -d)" \
 && curl -sSL -o "$TMP_PROTOC_DIR/protoc.zip" "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip" \
 && unzip -qo "$TMP_PROTOC_DIR/protoc.zip" -d "$TMP_PROTOC_DIR" \
 && install -m 0755 "$TMP_PROTOC_DIR/bin/protoc" /usr/local/bin/protoc \
 && cp -r "$TMP_PROTOC_DIR/include/." /usr/local/include/ \
 && rm -rf "$TMP_PROTOC_DIR"

# Python
ENV VIRTUAL_ENV=/opt/venv
ENV PATH="${VIRTUAL_ENV}/bin:${PATH}"

RUN python3 -m venv "${VIRTUAL_ENV}" \
 && "${VIRTUAL_ENV}/bin/pip" install --upgrade pip wheel setuptools \
 && "${VIRTUAL_ENV}/bin/pip" install \
      requests httpx redis psycopg[binary] PyYAML ujson

# Go toolchain utilities
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest \
 && GO111MODULE=on go install github.com/google/wire/cmd/wire@latest

# kubectl
ARG KUBECTL_VERSION=v1.34.1
RUN curl -fsSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
  && chmod +x /usr/local/bin/kubectl \
  && kubectl version --client --output=yaml || true

# codexctl
RUN mkdir -p /opt/codexctl
WORKDIR /opt/codexctl
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /usr/local/bin/codexctl ./cmd/codexctl

WORKDIR /workspace

CMD ["bash", "-lc", "codexctl --help || sleep infinity"]
