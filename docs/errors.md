# Errors

Test errors with `errors.Is` and `errors.As`, never by message.

| Error | Meaning |
|---|---|
| `ErrInvalid` | an argument was refused before any write was sent; the message names the value and what is allowed |
| `ErrNoReply` | the switch did not answer in time |
| `ErrWriteUnconfirmed`, `*UnconfirmedWriteError` | a write was sent and never answered: it may have been applied, once or several times (also matches `ErrNoReply`); read the setting back before deciding, see "When a write gets no answer" in [writing safely](writing-safely.md) |
| `ErrRejected`, `*RejectedError` | the switch answered with a non-zero code: it refused a write, would not report a setting or did not accept the session key; the error carries the code (`ReplyCode`) and, for the known ones, the reason |
| `ErrMoveUnconfirmed` | `SetIPConfig` or `EnableDHCP` lost track of the switch: look at both addresses (or look it up by its MAC) before believing either |
| `ErrNoInterface` | `Discover` has nowhere to send |
| `ErrInvalidConfigFile` | a configuration backup that is not complete, after the download was repeated |
| `ErrReplyTooLarge` | a fragmented reply bigger than any switch sends |
| `ErrWrongSwitch`, `*WrongSwitchError` | the device that answered is not the switch the client talks to (`Config.MAC`, or the MAC the switch reported on the first contact), or it answered without any MAC address; nothing was written to it |
| `ErrSwitchInUse` | another client of this process already talks to that switch (`Dial`, or the first call of a client made with an address when the other client names the switch by its MAC, or a move onto an address the other holds); close the first one or share it |
| `ErrClosed` | the client was closed (`Close` ends a client for good); make a new one with `Dial` |
| `ErrNotFound` | no switch with that MAC answered a discovery probe |
| `*safety.FirmwareError`, `*safety.PortUseError`, `*safety.ImpactError` | a gate refused; nothing was sent |
| `safety.ErrDryRun` | the dry-run gate recorded the write and sent nothing |
| `safety.ErrCannotCheckPorts` | `PortGate` could not read what the ports are used for, so it refused |
