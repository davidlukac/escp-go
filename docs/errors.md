# Errors

Test errors with `errors.Is` and `errors.As`, never by message.

| Error | Meaning |
|---|---|
| `ErrInvalid` | an argument was refused before any write was sent; the message names the value and what is allowed |
| `ErrNoReply` | the switch did not answer in time |
| `ErrRejected`, `*RejectedError` | the switch answered with a non-zero code: it refused a write, would not report a setting or did not accept the session key; the error carries the code (`ReplyCode`) and, for the known ones, the reason |
| `ErrMoveUnconfirmed` | `SetIPConfig` lost track of the switch: look at both addresses before believing either |
| `ErrNoInterface` | `Discover` has nowhere to send |
| `ErrInvalidConfigFile` | a configuration backup that is not complete, after the download was repeated |
| `ErrReplyTooLarge` | a fragmented reply bigger than any switch sends |
| `ErrWrongSwitch`, `*WrongSwitchError` | the device that answered is not the switch `Config.MAC` names; nothing was written to it |
| `ErrNotFound` | no switch with that MAC answered a discovery probe |
| `*safety.FirmwareError`, `*safety.PortUseError`, `*safety.ImpactError` | a gate refused; nothing was sent |
| `safety.ErrDryRun` | the dry-run gate recorded the write and sent nothing |
| `safety.ErrCannotCheckPorts` | `PortGate` could not read what the ports are used for, so it refused |
