FROM node:24-alpine AS build

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .

# Публичные значения SEO и аналитики: Vite встраивает их в сборку, пустые — теги не рендерятся.
ARG VITE_METRIKA_ID=""
ARG VITE_WEBMASTER_VERIFICATION=""
ENV VITE_METRIKA_ID=$VITE_METRIKA_ID \
    VITE_WEBMASTER_VERIFICATION=$VITE_WEBMASTER_VERIFICATION

RUN npm run build

FROM gcr.io/distroless/nodejs24-debian13:nonroot@sha256:6abfcb367ebc291572c774c1592a986be2ab26a2486f73093401327495a5962a

WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000 \
    HOST=0.0.0.0

COPY --from=build --chown=65532:65532 /app/.output ./.output

EXPOSE 3000
# Образ distroless: ENTRYPOINT уже `node`, поэтому только путь к скрипту (exec-форма).
CMD [".output/server/index.mjs"]
