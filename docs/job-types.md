# Typed job contracts (`job_type@version`)

Optional. Without it, every job type is untyped, exactly as before. With it, a job type has a name,
a version and JSON Schemas for its input and result, so producers and executors that are developed
separately can rely on each other.

## Turn it on

Put the contracts in a file and point the control plane at it:

```
JOB_TYPES_FILE=/etc/processor/job-types.json     # see job-types.example.json
JOB_TYPES_STRICT=true                            # optional: refuse job types that are not listed
```

```json
{"job_types": [
  {"name": "video.frames", "version": "1.2", "description": "…",
   "input_schema":  {"type": "object", "required": ["source", "fps"],
                     "properties": {"fps": {"type": "integer", "minimum": 1, "maximum": 30}}},
   "result_schema": {"type": "object", "required": ["frames"]}}
]}
```

A file that does not load stops the control plane at start-up (a typo must not silently switch the
checks off). Versions look like `1` or `1.2`; `0` is reserved for the untyped contract.

## Submitting

`POST /api/v1/jobs` accepts any of:

| Body | Meaning |
|---|---|
| `"job_type": "video.frames"` | The newest registered version (compared by number: 1.10 is newer than 1.9) |
| `"job_type": "video.frames@1.2"` | That version |
| `"job_type": "video.frames", "job_type_version": "1.2"` | The same |

* The input is checked against the version's `input_schema`; a mismatch is `400 invalid_input` and says where.
* An unknown version is `400 unknown_job_type_version`; with `JOB_TYPES_STRICT`, an unlisted type is `400 unknown_job_type`.
* A type that is not listed is accepted as version `"0"` (no checks), unless the registry is strict. Naming a
  version of an unlisted type (`ocr.page@3`) is refused: there is no contract to hold it to.
* The job records `job_type` (the bare name, which is what workers' capabilities match, so workers need no
  change) and `job_type_version`. Both are in job responses and in the worker's poll answer.
* An `Idempotency-Key` replay must match the type **and** version, otherwise `409 idempotency_conflict`.

## Results

When a worker completes a job whose version has a `result_schema`, the result is checked first. A result
that breaks the contract is **not** a success: the attempt is failed (and retried while attempts remain),
the error text says why, and the worker gets `422 invalid_result`.

## Looking around

`GET /api/v1/job-types` still returns `job_types` (the types online workers can run) and now also
`registry` (name, version, description, schemas, and whether a worker can run it now) and `strict`.

## The schema language

A small subset of JSON Schema, checked with no dependency: `type` (a name or a list), `enum`, `const`,
`properties`, `required`, `additionalProperties` (true, false or a schema), `items`, `minItems`, `maxItems`,
`minLength`, `maxLength`, `pattern`, `minimum`, `maximum`. Annotations (`$schema`, `title`, `description`,
`default`, `examples`) are ignored. Anything else (`oneOf`, `$ref`, `format`, …) is **refused when the file
loads**, never silently ignored, so a contract cannot promise a check that is not made.
