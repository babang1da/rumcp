FROM node:24-alpine AS build

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:24-alpine

WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000 \
    HOST=0.0.0.0
USER node

COPY --from=build --chown=node:node /app/.output ./.output

EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
