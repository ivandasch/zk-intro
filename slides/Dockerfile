FROM asciidoctor/docker-asciidoctor:latest

ARG MYUSER
RUN adduser --shell /bin/bash --disabled-password  --home /home/${MYUSER} ${MYUSER}
WORKDIR /home/${MYUSER}
USER $MYUSER
RUN mkdir ~/slides
