# Extensibility and Plugin Interfaces

English | [简体中文](../extensibility.md)

Related: [Documentation Index](./index.md) · [Import, Export, and Code Generation](./interop.md) · [Protocol Adapters](./protocols.md)

Even if v1 does not expose third-party plugins, internal extension points use registries and Go interfaces to reduce coupling, improve testability, and make future exposure possible:

| Extension Point | Interface | Registration |
|-----------------|-----------|--------------|
| Import format | `Importer` | Register by format ID in `ImporterRegistry` |
| Export format | `Exporter` | Same as above |
| Code generation | `CodeGen` | Register by `(lang, client)` |
| Authentication type | `AuthProvider` | Register by auth type; responsible for signing/injection |
| Dynamic variable | `DynamicVar` | Register a generator by `$name` |
| Protocol | `ProtocolSession` | Register by scheme |
| Assertion library | JS module injected into the script sandbox | Load when the engine starts |

- Every extension point consumes and produces IR or plain data, making unit testing straightforward.
- **Future third-party strategy**: plugins run in WASM or a sandboxed child process and communicate through a restricted host API, with no direct filesystem or network access.
