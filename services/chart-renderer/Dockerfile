FROM mcr.microsoft.com/playwright:v1.59.0-noble

WORKDIR /app

COPY package.json ./
RUN npm install --production && npx playwright install --with-deps chromium

COPY server.js chart.html ./

ENV PORT=3100
EXPOSE 3100

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:3100/health >/dev/null || exit 1

CMD ["node", "server.js"]
