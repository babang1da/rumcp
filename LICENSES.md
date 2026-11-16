# Third-party licenses

The connectors of RuMCP are written from the public API documentation of the
services. Open source MCP servers of the same services are used as a reference
for the tool set — which read-only questions an assistant should be able to ask
— never as source code: no code is copied, so no license of a reference
project restricts the RuMCP code. The projects that shaped the tool sets are
listed here for transparency.

## Reference projects

### DevCrabs-Inc/yandex-metrika-mcp — MIT

- Source: https://github.com/DevCrabs-Inc/yandex-metrika-mcp
- License: MIT
- Used for: the set of read-only Metrica tools (counters, table and bytime
  reports, goals, traffic sources) of `server/internal/connectors/metrika`.

MIT permits use, copy, modification and redistribution with the copyright and
permission notice; the RuMCP code in `server/internal/connectors/metrika` is
original, so nothing of this notice needs to travel with it.

## Dependencies

Go module dependencies carry their own licenses; the direct ones are:

| Module | License |
|--------|---------|
| github.com/modelcontextprotocol/go-sdk | MIT (moving to Apache-2.0) |
| github.com/google/jsonschema-go | MIT |
| github.com/jackc/pgx/v5 | MIT |
