FROM rockylinux/rockylinux:10.1

# Rocky 10 ships python3.12 in BaseOS; python3.13 comes from EPEL.
RUN dnf install -y epel-release \
    && dnf install -y python3.13 python3.13-devel python3.13-pip rpm-build \
    && dnf clean all

# Install uv (the project's package/build manager).
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:${PATH}"

WORKDIR /code
