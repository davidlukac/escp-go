## What and why

<!-- One paragraph. Link the issue if there is one. -->

## Checklist

Full list: [`CONTRIBUTING.md`](../CONTRIBUTING.md).

- [ ] Title follows Conventional Commits (`feat:`, `fix:`, `docs:`, `ci:`, ...)
- [ ] `make pre-commit` passes locally (format, vet, lint, tests with `-race`, coverage policy)
- [ ] Exported API changes are listed in `CHANGELOG.md` (`make apidiff`); a break is called out as one
- [ ] A write that could cut management access goes through the write gate and is covered by a test that sends nothing on a refusal
- [ ] `PROTOCOL.md` is current when the wire handling changed; `safety/compatibility.json` when a firmware or model was verified
- [ ] Hardware and firmware verified on are stated (or "not verified live")
- [ ] No credentials, MAC addresses or real addresses in code, tests or docs
