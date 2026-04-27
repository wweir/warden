# internal/setupbundle

## Responsibilities

`internal/setupbundle` owns self-extracting setup bundle encoding:

- Appends payload bytes and a fixed trailer to a bootstrap executable.
- Extracts the appended payload from an executable image.
- Validates trailer magic and payload length.

## Boundary

- The package does not install services or execute payloads.
- Callers decide where extracted bytes are written and how installation proceeds.
