# JSON Repair SSE Fixtures

These fixtures are sanitized reproductions of three malformed tool inputs seen
in exported provider sessions. They retain only tool-use SSE events, use
synthetic tool-call IDs, and replace argument content with non-sensitive text.

- `truncated-string-at-eof.sse` ends after an `input_json_delta`, reproducing a
  truncated string and object without inventing a provider stop event.
- `missing-object-close.sse` contains a completed tool block whose outer object
  terminator is missing.
- `unescaped-quotes.sse` contains quotation marks that were not escaped inside
  a string value.

Each `data` field is valid JSON. The concatenated `partial_json` value is
intentionally invalid and is the input exercised by the repair tests.
