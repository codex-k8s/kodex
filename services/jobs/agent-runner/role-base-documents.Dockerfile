ARG AGENT_RUNNER_IMAGE
FROM ${AGENT_RUNNER_IMAGE}

USER 0:0

RUN apt-get update \
	&& apt-get install --no-install-recommends -y \
		libreoffice-writer \
		poppler-utils \
		tesseract-ocr \
		tesseract-ocr-eng \
		tesseract-ocr-rus \
	&& rm -rf /var/lib/apt/lists/*

USER 10001:10001
