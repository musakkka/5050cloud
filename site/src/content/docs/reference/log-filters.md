---
title: Log filter syntax
description: Filter deploy, build and HTTP logs in the console and with `5050 logs -f` using Railway's filter syntax.
---

Filter deploy, build and HTTP logs in the console and with `5050 logs -f` using Railway's filter syntax.

## Grammar

```
expr  := term { [AND|OR] term }      adjacent terms mean AND
term  := [-] ( word | "phrase" | @attr:value | @attr:op number | @attr:a..b | replica:<id> | ( expr ) )
op    := > | >= | < | <=
```

Matching is case-insensitive. `-` negates a term.

## Attributes

- All logs: `level`, `service`, `replica`, `deployment`, `stream`, and any field of a JSON log line (dotted paths, `[i]` for arrays).
- HTTP logs: `method`, `path`, `host`, `httpStatus`, `responseTime` (alias `durationMs`), `srcIp`, `requestId`, `userAgent`, `bytes`.

## Examples

| Filter | Shows |
|---|---|
| `@level:error -healthcheck` | Errors that do not mention healthcheck |
| `@httpStatus:>=500 @path:/api` | Server errors under `/api` |
| `"connection refused" OR timeout` | Either text |
| `@responseTime:500..2000` | Requests that took 0.5 to 2 seconds |
| `@user.id:42` | JSON lines where `user.id` is 42 |

## Limits

Queries return up to 500 lines by default and 5000 at most. Logs are kept 7 days on each host, and at most 1 GB per service per host. If a host is offline, results include its last error and deploy lines with a note saying so.
