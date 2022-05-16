FROM rockylinux:8.5

RUN dnf install -y python38 python38-devel
RUN dnf install -y rpm-build
RUN pip3 install poetry==1.2.0a2
RUN alternatives --set python /usr/bin/python3.8

WORKDIR /code
