FROM mcr.microsoft.com/devcontainers/go:1.23

ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies
RUN apt-get update && \
    apt-get install -y \
    curl \
    ca-certificates \
    ripgrep \
    cmake make gcc git \
    fd-find

RUN mkdir /Library
RUN chown vscode:vscode /Library

USER vscode
