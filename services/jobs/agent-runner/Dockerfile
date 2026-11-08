FROM docker.io/library/golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS build

ENV GOTOOLCHAIN=local

RUN test "$(go env GOVERSION)" = "go1.26.10"

RUN mkdir -p /out /tmp/kodex-go-build-cache \
	&& printf '%s' 'cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImNyeXB0by9zaGEyNTYiCgkiZm10IgoJImlvIgoJIm9zIgoJIm9zL2V4ZWMiCgkib3Mvc2lnbmFsIgoJInBhdGgvZmlsZXBhdGgiCgkic29ydCIKCSJzdHJpbmdzIgoJInN5c2NhbGwiCikKCmNvbnN0ICgKCXRydXN0ZWRHT1JPT1QgICAgICAgPSAiL3Vzci9sb2NhbC9nbyIKCWJvb3RzdHJhcEdPUk9PVCAgICAgPSAiL29wdC9rb2RleC9ib290c3RyYXAtZ28iCglwcm90ZWN0ZWRTdGFnaW5nICAgID0gIi9vcHQva29kZXgvcHJvdGVjdGVkLWFydGlmYWN0cyIKCXByb3RlY3RlZFRhcmdldERpciAgPSAiL3Vzci9sb2NhbC9iaW4iCglwcm90ZWN0ZWRSb290ICAgICAgID0gIi8iCgl0cnVzdGVkVUlEICAgICAgICAgID0gMAoJdHJ1c3RlZEdJRCAgICAgICAgICA9IDAKCXByU2V0Q2hpbGRTdWJyZWFwZXIgPSAzNgoJbWF4TGludXhTaWduYWwgICAgICA9IDY0CglnbGliY1NpZ25hbENhbmNlbCAgID0gMzIKCWdsaWJjU2lnbmFsU2V0WElEICAgPSAzMwopCgp2YXIgcHJvdGVjdGVkVG9vbHMgPSBbXXN0cmluZ3sKCSJidWYiLAoJImdvZnVtcHQiLAoJImdvaW1wb3J0cyIsCgkiZ29sYW5nY2ktbGludCIsCgkiZ29vc2UiLAoJImdycGN1cmwiLAoJIm1vY2tnZW4iLAoJIm9hcGktY29kZWdlbiIsCgkicHJvdG9jLWdlbi1nbyIsCgkicHJvdG9jLWdlbi1nby1ncnBjIiwKCSJzcWxjIiwKCSJzdGF0aWNjaGVjayIsCgkieXEiLAp9CgpmdW5jIGZhaWwoZm9ybWF0IHN0cmluZywgdmFsdWVzIC4uLmFueSkgewoJZm10LkZwcmludGYob3MuU3RkZXJyLCBmb3JtYXQrIlxuIiwgdmFsdWVzLi4uKQoJb3MuRXhpdCgxKQp9CgpmdW5jIGJvb3RzdHJhcEd1YXJkUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRSb290LCAia29kZXgtZ28tdG9vbGNoYWluLWd1YXJkIikKfQoKZnVuYyBzZXJ2aWNlc0VudHJ5cG9pbnRQYXRoKCkgc3RyaW5nIHsKCXJldHVybiBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgImtvZGV4LWluaXQiKQp9CgpmdW5jIHNlcnZpY2VzUnVubmVyUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsICJrb2RleC1hZ2VudC1ydW5uZXIiKQp9CgpmdW5jIHRydXN0ZWRHb0NvbW1hbmQoZ29FeGVjdXRhYmxlIHN0cmluZywgYXJndW1lbnRzIC4uLnN0cmluZykgKmV4ZWMuQ21kIHsKCWNvbW1hbmQgOj0gZXhlYy5Db21tYW5kKGdvRXhlY3V0YWJsZSwgYXJndW1lbnRzLi4uKQoJY29tbWFuZC5FbnYgPSBbXXN0cmluZ3sKCQkiUEFUSD0vdXNyL2xvY2FsL2dvL2JpbjovdXNyL2JpbjovYmluIiwKCQkiSE9NRT0vdG1wIiwKCQkiR09ST09UPSIgKyB0cnVzdGVkR09ST09ULAoJCSJHT0VOVj1vZmYiLAoJCSJHT0ZMQUdTPSIsCgkJIkdPVE9PTENIQUlOPWxvY2FsIiwKCQkiR09XT1JLPW9mZiIsCgl9CglyZXR1cm4gY29tbWFuZAp9CgpmdW5jIGV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsIGxhYmVsLCBleHBlY3RlZCBzdHJpbmcsIGFyZ3VtZW50cyAuLi5zdHJpbmcpIHsKCW91dHB1dCwgZXJyIDo9IHRydXN0ZWRHb0NvbW1hbmQoZ29FeGVjdXRhYmxlLCBhcmd1bWVudHMuLi4pLk91dHB1dCgpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCIlcyBmYWlsZWQ6ICV2IiwgbGFiZWwsIGVycikKCX0KCWlmICFieXRlcy5FcXVhbChvdXRwdXQsIFtdYnl0ZShleHBlY3RlZCkpIHsKCQlmYWlsKCIlcyBtaXNtYXRjaDogZ290ICVxLCB3YW50ICVxIiwgbGFiZWwsIG91dHB1dCwgZXhwZWN0ZWQpCgl9Cn0KCmZ1bmMgdmVyaWZ5R28oZ29FeGVjdXRhYmxlIHN0cmluZykgewoJZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgIkdPVkVSU0lPTiIsICJnbzEuMjYuMTBcbiIsICJlbnYiLCAiR09WRVJTSU9OIikKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1RPT0xDSEFJTiIsICJsb2NhbFxuIiwgImVudiIsICJHT1RPT0xDSEFJTiIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09ST09UIiwgdHJ1c3RlZEdPUk9PVCsiXG4iLCAiZW52IiwgIkdPUk9PVCIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiZ28gdG9vbCBjb21waWxlIiwgImNvbXBpbGUgdmVyc2lvbiBnbzEuMjYuMTBcbiIsICJ0b29sIiwgImNvbXBpbGUiLCAiLVY9ZnVsbCIpCn0KCmZ1bmMgcHJvdGVjdGVkTmFtZXMocHJvZmlsZSBzdHJpbmcpIFtdc3RyaW5nIHsKCW5hbWVzIDo9IGFwcGVuZChbXXN0cmluZyhuaWwpLCBwcm90ZWN0ZWRUb29scy4uLikKCXN3aXRjaCBwcm9maWxlIHsKCWNhc2UgInNlcnZpY2VzIjoKCQluYW1lcyA9IGFwcGVuZChuYW1lcywgImtvZGV4LWFnZW50LXJ1bm5lciIsICJrb2RleC1pbml0IikKCWNhc2UgImRlcGxveSI6CgkJbmFtZXMgPSBhcHBlbmQobmFtZXMsICJidXN5Ym94IikKCWRlZmF1bHQ6CgkJZmFpbCgidW5zdXBwb3J0ZWQgcHJvZmlsZSAlcSIsIHByb2ZpbGUpCgl9Cglzb3J0LlN0cmluZ3MobmFtZXMpCglyZXR1cm4gbmFtZXMKfQoKZnVuYyByZXF1aXJlVHJ1c3RlZElkZW50aXR5KCkgewoJaWYgb3MuR2V0ZXVpZCgpICE9IHRydXN0ZWRVSUQgfHwgb3MuR2V0ZWdpZCgpICE9IHRydXN0ZWRHSUQgewoJCWZhaWwoInRydXN0ZWQgZ3VhcmQgcmVxdWlyZXMgdWlkOmdpZCAlZDolZCIsIHRydXN0ZWRVSUQsIHRydXN0ZWRHSUQpCgl9Cn0KCmZ1bmMgc3RhdChwYXRoIHN0cmluZykgKG9zLkZpbGVJbmZvLCAqc3lzY2FsbC5TdGF0X3QpIHsKCWluZm8sIGVyciA6PSBvcy5Mc3RhdChwYXRoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgibHN0YXQgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJc3RhdHVzLCBvayA6PSBpbmZvLlN5cygpLigqc3lzY2FsbC5TdGF0X3QpCglpZiAhb2sgewoJCWZhaWwoInN0YXQgJXM6IHVuc3VwcG9ydGVkIHBsYXRmb3JtIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyBlbnN1cmVEaXJlY3RvcnkocGF0aCBzdHJpbmcpIHsKCXBhdGggPSBmaWxlcGF0aC5DbGVhbihwYXRoKQoJcm9vdCA6PSBmaWxlcGF0aC5DbGVhbihwcm90ZWN0ZWRSb290KQoJaWYgcGF0aCA9PSByb290IHsKCQl2ZXJpZnlEaXJlY3RvcnkocGF0aCkKCQlyZXR1cm4KCX0KCWlmIHJvb3QgIT0gIi8iICYmICFzdHJpbmdzLkhhc1ByZWZpeChwYXRoLCByb290K3N0cmluZyhvcy5QYXRoU2VwYXJhdG9yKSkgewoJCWZhaWwoInByb3RlY3RlZCBwYXRoICVzIGVzY2FwZXMgcm9vdCAlcyIsIHBhdGgsIHJvb3QpCgl9CglwYXJlbnQgOj0gZmlsZXBhdGguRGlyKHBhdGgpCgllbnN1cmVEaXJlY3RvcnkocGFyZW50KQoJaW5mbywgZXJyIDo9IG9zLkxzdGF0KHBhdGgpCglpZiBlcnIgPT0gbmlsICYmICFpbmZvLklzRGlyKCkgewoJCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocGF0aCk7IGVyciAhPSBuaWwgewoJCQlmYWlsKCJyZW1vdmUgdW5zYWZlIHBhdGggJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCQlpbmZvID0gbmlsCgl9IGVsc2UgaWYgZXJyICE9IG5pbCAmJiAhb3MuSXNOb3RFeGlzdChlcnIpIHsKCQlmYWlsKCJsc3RhdCAlczogJXYiLCBwYXRoLCBlcnIpCgl9CglpZiBpbmZvID09IG5pbCB7CgkJaWYgZXJyIDo9IG9zLk1rZGlyKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCAmJiAhb3MuSXNFeGlzdChlcnIpIHsKCQkJZmFpbCgibWtkaXIgJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCX0KCWlmIGVyciA6PSBvcy5DaG93bihwYXRoLCB0cnVzdGVkVUlELCB0cnVzdGVkR0lEKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2hvd24gJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLkNobW9kKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2htb2QgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJdmVyaWZ5RGlyZWN0b3J5KHBhdGgpCn0KCmZ1bmMgdmVyaWZ5RGlyZWN0b3J5KHBhdGggc3RyaW5nKSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uSXNEaXIoKSB8fCBpbmZvLk1vZGUoKSZvcy5Nb2RlU3ltbGluayAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCBhIHJlYWwgZGlyZWN0b3J5IiwgcGF0aCkKCX0KCWlmIHN0YXR1cy5VaWQgIT0gdWludDMyKHRydXN0ZWRVSUQpIHx8IHN0YXR1cy5HaWQgIT0gdWludDMyKHRydXN0ZWRHSUQpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCByb290LW93bmVkIiwgcGF0aCkKCX0KCWlmIGluZm8uTW9kZSgpLlBlcm0oKSYwbzAyMiAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KfQoKZnVuYyB2ZXJpZnlQYXJlbnRzKHBhdGggc3RyaW5nKSB7Cglyb290IDo9IGZpbGVwYXRoLkNsZWFuKHByb3RlY3RlZFJvb3QpCglmb3IgY3VycmVudCA6PSBmaWxlcGF0aC5DbGVhbihwYXRoKTsgOyBjdXJyZW50ID0gZmlsZXBhdGguRGlyKGN1cnJlbnQpIHsKCQl2ZXJpZnlEaXJlY3RvcnkoY3VycmVudCkKCQlpZiBjdXJyZW50ID09IHJvb3QgewoJCQlyZXR1cm4KCQl9Cgl9Cn0KCmZ1bmMgcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUgc3RyaW5nKSB7Cglmb3IgXywgbmFtZSA6PSByYW5nZSBwcm90ZWN0ZWROYW1lcyhwcm9maWxlKSB7CgkJcGF0aCA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgbmFtZSkKCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHBhdGgpOyBlcnIgIT0gbmlsIHsKCQkJZmFpbCgicmVtb3ZlIHByb3RlY3RlZCBkZXN0aW5hdGlvbiAlczogJXYiLCBwYXRoLCBlcnIpCgkJfQoJfQp9CgpmdW5jIHByZXBhcmUocHJvZmlsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJdmVyaWZ5Qm9vdHN0cmFwR3VhcmQoKQoJZW5zdXJlRGlyZWN0b3J5KGZpbGVwYXRoLkRpcih0cnVzdGVkR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIoYm9vdHN0cmFwR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIocHJvdGVjdGVkU3RhZ2luZykpCgllbnN1cmVEaXJlY3RvcnkocHJvdGVjdGVkVGFyZ2V0RGlyKQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbCh0cnVzdGVkR09ST09UKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xlYW4gJXM6ICV2IiwgdHJ1c3RlZEdPUk9PVCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbChib290c3RyYXBHT1JPT1QpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbGVhbiAlczogJXYiLCBib290c3RyYXBHT1JPT1QsIGVycikKCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocHJvdGVjdGVkU3RhZ2luZyk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNsZWFuICVzOiAldiIsIHByb3RlY3RlZFN0YWdpbmcsIGVycikKCX0KCWVuc3VyZURpcmVjdG9yeSh0cnVzdGVkR09ST09UKQoJZW5zdXJlRGlyZWN0b3J5KHByb3RlY3RlZFN0YWdpbmcpCglyZW1vdmVQcm90ZWN0ZWRMZWF2ZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHModHJ1c3RlZEdPUk9PVCkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQp9CgpmdW5jIHZlcmlmeVJlZ3VsYXIocGF0aCBzdHJpbmcpIChvcy5GaWxlSW5mbywgKnN5c2NhbGwuU3RhdF90KSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uTW9kZSgpLklzUmVndWxhcigpIHx8IGluZm8uTW9kZSgpJm9zLk1vZGVTeW1saW5rICE9IDAgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3QgYSByZWd1bGFyIG5vbi1zeW1saW5rIGZpbGUiLCBwYXRoKQoJfQoJaWYgc3RhdHVzLlVpZCAhPSB1aW50MzIodHJ1c3RlZFVJRCkgfHwgc3RhdHVzLkdpZCAhPSB1aW50MzIodHJ1c3RlZEdJRCkgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3Qgcm9vdC1vd25lZCIsIHBhdGgpCgl9CglpZiBzdGF0dXMuTmxpbmsgIT0gMSB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGhhcyAlZCBoYXJkbGlua3MiLCBwYXRoLCBzdGF0dXMuTmxpbmspCgl9CglpZiBpbmZvLk1vZGUoKS5QZXJtKCkmMG8wMjIgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyB2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHBhdGggc3RyaW5nKSB7CglpbmZvLCBfIDo9IHZlcmlmeVJlZ3VsYXIocGF0aCkKCWlmIGluZm8uTW9kZSgpLlBlcm0oKSAhPSAwbzU1NSB7CgkJZmFpbCgicHJvdGVjdGVkIGV4ZWN1dGFibGUgJXMgaGFzIG1vZGUgJTA0bywgd2FudCAwNTU1IiwgcGF0aCwgaW5mby5Nb2RlKCkuUGVybSgpKQoJfQoJdmVyaWZ5UGFyZW50cyhmaWxlcGF0aC5EaXIocGF0aCkpCn0KCmZ1bmMgZGlnZXN0KHBhdGggc3RyaW5nKSBbc2hhMjU2LlNpemVdYnl0ZSB7CglmaWxlLCBlcnIgOj0gb3MuT3BlbihwYXRoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgib3BlbiAlczogJXYiLCBwYXRoLCBlcnIpCgl9CglkZWZlciBmaWxlLkNsb3NlKCkKCWhhc2ggOj0gc2hhMjU2Lk5ldygpCglpZiBfLCBlcnIgOj0gaW8uQ29weShoYXNoLCBmaWxlKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiaGFzaCAlczogJXYiLCBwYXRoLCBlcnIpCgl9Cgl2YXIgcmVzdWx0IFtzaGEyNTYuU2l6ZV1ieXRlCgljb3B5KHJlc3VsdFs6XSwgaGFzaC5TdW0obmlsKSkKCXJldHVybiByZXN1bHQKfQoKZnVuYyB2ZXJpZnlCb290c3RyYXBHdWFyZCgpIHsKCXBhdGggOj0gYm9vdHN0cmFwR3VhcmRQYXRoKCkKCXZlcmlmeUluc3RhbGxlZEV4ZWN1dGFibGUocGF0aCkKCWV4ZWN1dGFibGUsIGVyciA6PSBvcy5FeGVjdXRhYmxlKCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoInJlc29sdmUgcnVubmluZyBndWFyZDogJXYiLCBlcnIpCgl9CglpZiBkaWdlc3QocGF0aCkgIT0gZGlnZXN0KGV4ZWN1dGFibGUpIHsKCQlmYWlsKCJydW5uaW5nIGd1YXJkIGRpZ2VzdCBkaWZmZXJzIGZyb20gcHJvdGVjdGVkIGJvb3RzdHJhcCBndWFyZCIpCgl9Cn0KCmZ1bmMgY29weVByb3RlY3RlZChzb3VyY2UsIHRhcmdldCBzdHJpbmcpIHsKCXNvdXJjZUZpbGUsIGVyciA6PSBvcy5PcGVuKHNvdXJjZSkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoIm9wZW4gcHJvdGVjdGVkIHNvdXJjZSAlczogJXYiLCBzb3VyY2UsIGVycikKCX0KCWRlZmVyIHNvdXJjZUZpbGUuQ2xvc2UoKQoJdGFyZ2V0RmlsZSwgZXJyIDo9IG9zLk9wZW5GaWxlKHRhcmdldCwgb3MuT19XUk9OTFl8b3MuT19DUkVBVEV8b3MuT19FWENMLCAwbzU1NSkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoImNyZWF0ZSBwcm90ZWN0ZWQgZGVzdGluYXRpb24gJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9CglpZiBfLCBlcnIgOj0gaW8uQ29weSh0YXJnZXRGaWxlLCBzb3VyY2VGaWxlKTsgZXJyICE9IG5pbCB7CgkJdGFyZ2V0RmlsZS5DbG9zZSgpCgkJZmFpbCgiY29weSBwcm90ZWN0ZWQgYXJ0aWZhY3QgJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9CglpZiBlcnIgOj0gdGFyZ2V0RmlsZS5TeW5jKCk7IGVyciAhPSBuaWwgewoJCXRhcmdldEZpbGUuQ2xvc2UoKQoJCWZhaWwoInN5bmMgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuQ2htb2QoMG81NTUpOyBlcnIgIT0gbmlsIHsKCQl0YXJnZXRGaWxlLkNsb3NlKCkKCQlmYWlsKCJjaG1vZCBwcm90ZWN0ZWQgYXJ0aWZhY3QgJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9CglpZiBlcnIgOj0gdGFyZ2V0RmlsZS5DbG9zZSgpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbG9zZSBwcm90ZWN0ZWQgYXJ0aWZhY3QgJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9Cn0KCmZ1bmMgdmVyaWZ5RXhhY3RTdGFnaW5nKG5hbWVzIFtdc3RyaW5nKSB7CgllbnRyaWVzLCBlcnIgOj0gb3MuUmVhZERpcihwcm90ZWN0ZWRTdGFnaW5nKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgicmVhZCBwcm90ZWN0ZWQgc3RhZ2luZzogJXYiLCBlcnIpCgl9CglpZiBsZW4oZW50cmllcykgIT0gbGVuKG5hbWVzKSB7CgkJZmFpbCgicHJvdGVjdGVkIHN0YWdpbmcgaGFzICVkIGFydGlmYWN0cywgd2FudCAlZCIsIGxlbihlbnRyaWVzKSwgbGVuKG5hbWVzKSkKCX0KCWZvciBpbmRleCwgZW50cnkgOj0gcmFuZ2UgZW50cmllcyB7CgkJaWYgZW50cnkuTmFtZSgpICE9IG5hbWVzW2luZGV4XSB7CgkJCWZhaWwoInByb3RlY3RlZCBzdGFnaW5nIGFydGlmYWN0ICVxLCB3YW50ICVxIiwgZW50cnkuTmFtZSgpLCBuYW1lc1tpbmRleF0pCgkJfQoJfQp9CgpmdW5jIGluc3RhbGwocHJvZmlsZSwgZ29FeGVjdXRhYmxlIHN0cmluZykgewoJcmVxdWlyZVRydXN0ZWRJZGVudGl0eSgpCgl2ZXJpZnlCb290c3RyYXBHdWFyZCgpCgluYW1lcyA6PSBwcm90ZWN0ZWROYW1lcyhwcm9maWxlKQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRTdGFnaW5nKQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRUYXJnZXREaXIpCgl2ZXJpZnlQYXJlbnRzKHRydXN0ZWRHT1JPT1QpCgl2ZXJpZnlFeGFjdFN0YWdpbmcobmFtZXMpCglzb3VyY2VEaWdlc3RzIDo9IG1ha2UobWFwW3N0cmluZ11bc2hhMjU2LlNpemVdYnl0ZSwgbGVuKG5hbWVzKSkKCWZvciBfLCBuYW1lIDo9IHJhbmdlIG5hbWVzIHsKCQlzb3VyY2UgOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRTdGFnaW5nLCBuYW1lKQoJCXZlcmlmeVJlZ3VsYXIoc291cmNlKQoJCXNvdXJjZURpZ2VzdHNbbmFtZV0gPSBkaWdlc3Qoc291cmNlKQoJfQoJcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJc291cmNlIDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkU3RhZ2luZywgbmFtZSkKCQl0YXJnZXQgOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsIG5hbWUpCgkJY29weVByb3RlY3RlZChzb3VyY2UsIHRhcmdldCkKCQl2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHRhcmdldCkKCQlpZiBkaWdlc3QodGFyZ2V0KSAhPSBzb3VyY2VEaWdlc3RzW25hbWVdIHsKCQkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0IGRpZ2VzdCBtaXNtYXRjaCBmb3IgJXMiLCBuYW1lKQoJCX0KCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocHJvdGVjdGVkU3RhZ2luZyk7IGVyciAhPSBuaWwgewoJCWZhaWwoInJlbW92ZSBwcm90ZWN0ZWQgc3RhZ2luZzogJXYiLCBlcnIpCgl9Cgl2ZXJpZnlQYXJlbnRzKHByb3RlY3RlZFRhcmdldERpcikKCWZvciBfLCBuYW1lIDo9IHJhbmdlIG5hbWVzIHsKCQl0YXJnZXQgOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsIG5hbWUpCgkJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZSh0YXJnZXQpCgkJaWYgZGlnZXN0KHRhcmdldCkgIT0gc291cmNlRGlnZXN0c1tuYW1lXSB7CgkJCWZhaWwoImZpbmFsIHByb3RlY3RlZCBhcnRpZmFjdCBkaWdlc3QgbWlzbWF0Y2ggZm9yICVzIiwgbmFtZSkKCQl9Cgl9Cgl2ZXJpZnlSZWd1bGFyKGdvRXhlY3V0YWJsZSkKCXZlcmlmeUdvKGdvRXhlY3V0YWJsZSkKfQoKZnVuYyBlbmFibGVTdWJyZWFwZXIoKSB7CglfLCBfLCBlcnJubyA6PSBzeXNjYWxsLlJhd1N5c2NhbGw2KHN5c2NhbGwuU1lTX1BSQ1RMLCBwclNldENoaWxkU3VicmVhcGVyLCAxLCAwLCAwLCAwLCAwKQoJaWYgZXJybm8gIT0gMCB7CgkJZmFpbCgiZW5hYmxlIGNoaWxkIHN1YnJlYXBlcjogJXYiLCBlcnJubykKCX0KfQoKZnVuYyBwcm94eVNpZ25hbHMoKSBbXW9zLlNpZ25hbCB7CglzaWduYWxzIDo9IG1ha2UoW11vcy5TaWduYWwsIDAsIG1heExpbnV4U2lnbmFsLTcpCglmb3IgbnVtYmVyIDo9IDE7IG51bWJlciA8PSBtYXhMaW51eFNpZ25hbDsgbnVtYmVyKysgewoJCWN1cnJlbnQgOj0gc3lzY2FsbC5TaWduYWwobnVtYmVyKQoJCXN3aXRjaCBjdXJyZW50IHsKCQljYXNlIHN5c2NhbGwuU0lHS0lMTCwKCQkJc3lzY2FsbC5TSUdTVE9QLAoJCQlzeXNjYWxsLlNJR0NITEQsCgkJCXN5c2NhbGwuU0lHVVJHLAoJCQlzeXNjYWxsLlNJR1BST0YsCgkJCXN5c2NhbGwuU2lnbmFsKGdsaWJjU2lnbmFsQ2FuY2VsKSwKCQkJc3lzY2FsbC5TaWduYWwoZ2xpYmNTaWduYWxTZXRYSUQpOgoJCQljb250aW51ZQoJCWRlZmF1bHQ6CgkJCXNpZ25hbHMgPSBhcHBlbmQoc2lnbmFscywgY3VycmVudCkKCQl9Cgl9CglyZXR1cm4gc2lnbmFscwp9CgpmdW5jIGZvcndhcmRTaWduYWxzKHNpZ25hbHMgPC1jaGFuIG9zLlNpZ25hbCwgcHJvY2Vzc0dyb3VwIGludCwgc3RvcCA8LWNoYW4gc3RydWN0e30sIGRvbmUgY2hhbjwtIHN0cnVjdHt9KSB7CglkZWZlciBjbG9zZShkb25lKQoJZm9yIHsKCQlzZWxlY3QgewoJCWNhc2UgPC1zdG9wOgoJCQlyZXR1cm4KCQlkZWZhdWx0OgoJCX0KCQlzZWxlY3QgewoJCWNhc2UgcmVjZWl2ZWQgOj0gPC1zaWduYWxzOgoJCQlpZiByZWNlaXZlZCA9PSBuaWwgewoJCQkJY29udGludWUKCQkJfQoJCQlpZiB0eXBlZCwgb2sgOj0gcmVjZWl2ZWQuKHN5c2NhbGwuU2lnbmFsKTsgb2sgewoJCQkJc2VsZWN0IHsKCQkJCWNhc2UgPC1zdG9wOgoJCQkJCXJldHVybgoJCQkJZGVmYXVsdDoKCQkJCX0KCQkJCV8gPSBzeXNjYWxsLktpbGwoLXByb2Nlc3NHcm91cCwgdHlwZWQpCgkJCX0KCQljYXNlIDwtc3RvcDoKCQkJcmV0dXJuCgkJfQoJfQp9CgpmdW5jIHJlYXBFeGl0ZWRDaGlsZHJlbigpIHsKCWZvciB7CgkJdmFyIHN0YXR1cyBzeXNjYWxsLldhaXRTdGF0dXMKCQlwaWQsIGVyciA6PSBzeXNjYWxsLldhaXQ0KC0xLCAmc3RhdHVzLCBzeXNjYWxsLldOT0hBTkcsIG5pbCkKCQlzd2l0Y2ggewoJCWNhc2UgZXJyID09IHN5c2NhbGwuRUlOVFI6CgkJCWNvbnRpbnVlCgkJY2FzZSBlcnIgPT0gc3lzY2FsbC5FQ0hJTEQ6CgkJCXJldHVybgoJCWNhc2UgZXJyICE9IG5pbDoKCQkJZmFpbCgicmVhcCBleGl0ZWQgY2hpbGQ6ICV2IiwgZXJyKQoJCWNhc2UgcGlkID09IDA6CgkJCXJldHVybgoJCX0KCX0KfQoKZnVuYyBleGl0Q29kZShzdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzKSBpbnQgewoJaWYgc3RhdHVzLkV4aXRlZCgpIHsKCQlyZXR1cm4gc3RhdHVzLkV4aXRTdGF0dXMoKQoJfQoJaWYgc3RhdHVzLlNpZ25hbGVkKCkgewoJCXJldHVybiAxMjggKyBpbnQoc3RhdHVzLlNpZ25hbCgpKQoJfQoJcmV0dXJuIDEKfQoKZnVuYyBydW5TZXJ2aWNlc0VudHJ5cG9pbnQocnVubmVyIHN0cmluZywgYXJndW1lbnRzIFtdc3RyaW5nKSB7CglpZiBydW5uZXIgIT0gc2VydmljZXNSdW5uZXJQYXRoKCkgewoJCWZhaWwoInByb3RlY3RlZCBlbnRyeXBvaW50IHJlcXVpcmVzIGV4YWN0IHJ1bm5lciAlcyIsIHNlcnZpY2VzUnVubmVyUGF0aCgpKQoJfQoJaWYgZmlsZXBhdGguQ2xlYW4ob3MuQXJnc1swXSkgIT0gc2VydmljZXNFbnRyeXBvaW50UGF0aCgpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZW50cnlwb2ludCBtdXN0IHJ1biBmcm9tICVzIiwgc2VydmljZXNFbnRyeXBvaW50UGF0aCgpKQoJfQoJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZShzZXJ2aWNlc0VudHJ5cG9pbnRQYXRoKCkpCgl2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHJ1bm5lcikKCWVuYWJsZVN1YnJlYXBlcigpCgoJY29tbWFuZCA6PSBleGVjLkNvbW1hbmQocnVubmVyLCBhcmd1bWVudHMuLi4pCgljb21tYW5kLkVudiA9IG9zLkVudmlyb24oKQoJY29tbWFuZC5TdGRpbiA9IG9zLlN0ZGluCgljb21tYW5kLlN0ZG91dCA9IG9zLlN0ZG91dAoJY29tbWFuZC5TdGRlcnIgPSBvcy5TdGRlcnIKCWNvbW1hbmQuU3lzUHJvY0F0dHIgPSAmc3lzY2FsbC5TeXNQcm9jQXR0cntTZXRwZ2lkOiB0cnVlfQoKCXByb3hpZWRTaWduYWxzIDo9IHByb3h5U2lnbmFscygpCglzaWduYWxzIDo9IG1ha2UoY2hhbiBvcy5TaWduYWwsIGxlbihwcm94aWVkU2lnbmFscykqMikKCXNpZ25hbC5Ob3RpZnkoc2lnbmFscywgcHJveGllZFNpZ25hbHMuLi4pCglpZiBlcnIgOj0gY29tbWFuZC5TdGFydCgpOyBlcnIgIT0gbmlsIHsKCQlzaWduYWwuU3RvcChzaWduYWxzKQoJCWZhaWwoInN0YXJ0IHByb3RlY3RlZCBydW5uZXI6ICV2IiwgZXJyKQoJfQoKCXN0b3BGb3J3YXJkaW5nIDo9IG1ha2UoY2hhbiBzdHJ1Y3R7fSkKCWZvcndhcmRpbmdEb25lIDo9IG1ha2UoY2hhbiBzdHJ1Y3R7fSkKCWdvIGZvcndhcmRTaWduYWxzKHNpZ25hbHMsIGNvbW1hbmQuUHJvY2Vzcy5QaWQsIHN0b3BGb3J3YXJkaW5nLCBmb3J3YXJkaW5nRG9uZSkKCXZhciBydW5uZXJTdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzCglmb3IgewoJCXZhciBzdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzCgkJcGlkLCBlcnIgOj0gc3lzY2FsbC5XYWl0NCgtMSwgJnN0YXR1cywgMCwgbmlsKQoJCWlmIGVyciA9PSBzeXNjYWxsLkVJTlRSIHsKCQkJY29udGludWUKCQl9CgkJaWYgZXJyICE9IG5pbCB7CgkJCWNsb3NlKHN0b3BGb3J3YXJkaW5nKQoJCQlzaWduYWwuU3RvcChzaWduYWxzKQoJCQk8LWZvcndhcmRpbmdEb25lCgkJCWZhaWwoIndhaXQgZm9yIHByb3RlY3RlZCBydW5uZXI6ICV2IiwgZXJyKQoJCX0KCQlpZiBwaWQgPT0gY29tbWFuZC5Qcm9jZXNzLlBpZCB7CgkJCXJ1bm5lclN0YXR1cyA9IHN0YXR1cwoJCQlicmVhawoJCX0KCX0KCWNsb3NlKHN0b3BGb3J3YXJkaW5nKQoJc2lnbmFsLlN0b3Aoc2lnbmFscykKCTwtZm9yd2FyZGluZ0RvbmUKCXJlYXBFeGl0ZWRDaGlsZHJlbigpCglvcy5FeGl0KGV4aXRDb2RlKHJ1bm5lclN0YXR1cykpCn0KCmZ1bmMgbWFpbigpIHsKCWlmIGxlbihvcy5BcmdzKSA9PSAzICYmIG9zLkFyZ3NbMV0gPT0gInByZXBhcmUiIHsKCQlwcmVwYXJlKG9zLkFyZ3NbMl0pCgkJcmV0dXJuCgl9CglpZiBsZW4ob3MuQXJncykgPT0gNCAmJiBvcy5BcmdzWzFdID09ICJpbnN0YWxsIiB7CgkJaW5zdGFsbChvcy5BcmdzWzJdLCBvcy5BcmdzWzNdKQoJCXJldHVybgoJfQoJaWYgbGVuKG9zLkFyZ3MpID09IDMgJiYgb3MuQXJnc1sxXSA9PSAidmVyaWZ5IiB7CgkJdmVyaWZ5R28ob3MuQXJnc1syXSkKCQlyZXR1cm4KCX0KCWlmIGxlbihvcy5BcmdzKSA+PSAzICYmIG9zLkFyZ3NbMV0gPT0gImVudHJ5cG9pbnQiIHsKCQlydW5TZXJ2aWNlc0VudHJ5cG9pbnQob3MuQXJnc1syXSwgb3MuQXJnc1szOl0pCgkJcmV0dXJuCgl9CglmYWlsKCJ1bnN1cHBvcnRlZCBpbnZvY2F0aW9uIikKfQo=' | base64 -d > /tmp/kodex-go-toolchain-guard.go \
	&& /usr/bin/env -i PATH=/usr/local/go/bin:/usr/bin:/bin HOME=/tmp GOCACHE=/tmp/kodex-go-build-cache GOENV=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -buildvcs=false -o /out/kodex-go-toolchain-guard /tmp/kodex-go-toolchain-guard.go \
	&& rm -rf /tmp/kodex-go-toolchain-guard.go /tmp/kodex-go-build-cache

FROM docker.io/library/golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS runner-build

ENV GOTOOLCHAIN=local

WORKDIR /src/services/jobs/agent-runner

COPY services/jobs/agent-runner /src/services/jobs/agent-runner
COPY libs/go /src/libs/go/

RUN go mod download

RUN test "$(go env GOVERSION)" = "go1.26.10"
RUN --mount=type=tmpfs,target=/tmp,size=4294967296 \
	export GOCACHE=/tmp/go-build-cache GOTMPDIR=/tmp/go-tmp GOMAXPROCS=2 GOMEMLIMIT=768MiB \
	&& mkdir -p /out/kodex-protected "$GOCACHE" "$GOTMPDIR" \
	&& CGO_ENABLED=0 go build -p=1 -trimpath -buildvcs=false -ldflags='-X main.buildVersion=0.1.0' -o /out/kodex-protected/kodex-agent-runner ./cmd/agent-runner

# Локальный runtime сохраняет тот же защищенный entrypoint и UID-контракт, но
# не собирает полный набор инструментов роли. Production-образ остается
# последним stage этого Dockerfile и не зависит от local-runtime.
FROM docker.io/library/node:24.22.1-bookworm-slim@sha256:be8204d4bd327363357f532187b752ff62759c31bedf98e9e18fc08c9cbd605c AS local-codex-cli

ARG KODEX_CODEX_PACKAGE=@openai/codex@0.160.0
RUN set -eu; \
	npm install --global "${KODEX_CODEX_PACKAGE}"; \
	binary=$(find /usr/local/lib/node_modules/@openai/codex -type f \
		-path '*/vendor/*/bin/codex' -print -quit); \
	code_mode_host=$(find /usr/local/lib/node_modules/@openai/codex -type f \
		-path '*/vendor/*/bin/codex-code-mode-host' -print -quit); \
	test -n "$binary" && test -x "$binary"; \
	test -n "$code_mode_host" && test -x "$code_mode_host"; \
	"$binary" --version >/dev/null; \
	"$code_mode_host" --help >/dev/null; \
	install -D -m 0555 "$binary" /out/codex; \
	install -D -m 0555 "$code_mode_host" /out/codex-code-mode-host; \
	npm cache clean --force

FROM docker.io/library/debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS local-runtime

# Явное включение libpcre2 обновляет уже установленную в pinned base библиотеку:
# иначе установка новых инструментов оставляет её прежнюю уязвимую версию.
RUN set -eu; \
	apt-get update; \
	apt-get install -y --no-install-recommends bubblewrap ca-certificates git libpcre2-8-0 openssh-client tini; \
	groupadd --gid 10001 kodex; \
	useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin kodex; \
	mkdir -p /home/kodex /workspace /codex-home /run/secrets/kodex/runtime/provider; \
	touch /run/secrets/kodex/runtime/provider/auth.json /run/secrets/kodex/runtime/provider/auth.sha256; \
	chmod 0755 /run/secrets/kodex/runtime/provider; \
	chmod 0400 /run/secrets/kodex/runtime/provider/auth.json /run/secrets/kodex/runtime/provider/auth.sha256; \
	chown -R 10001:10001 /home/kodex /workspace /codex-home; \
	rm -rf /var/lib/apt/lists/* /root/.cache

COPY --from=local-codex-cli --chmod=0555 /out/codex /usr/local/bin/codex
COPY --from=local-codex-cli --chmod=0555 /out/codex-code-mode-host /usr/local/bin/codex-code-mode-host
# CODEX_SHELL должен уметь исполнять JavaScript, который платформа передаёт
# агенту для штатной проверки workspace. Native Codex binaries не включают
# отдельный Node.js runtime.
COPY --from=local-codex-cli --chmod=0555 /usr/local/bin/node /usr/local/bin/node
COPY --chmod=0555 --from=build /out/kodex-go-toolchain-guard /usr/local/bin/kodex-init
COPY --chmod=0555 --from=runner-build /out/kodex-protected/kodex-agent-runner /usr/local/bin/kodex-agent-runner

RUN node --version >/dev/null \
	&& codex --version >/dev/null \
	&& codex-code-mode-host --help >/dev/null

ENV HOME=/home/kodex \
	NPM_CONFIG_CACHE=/home/kodex/.npm \
	XDG_CONFIG_HOME=/home/kodex/.config \
	XDG_CACHE_HOME=/home/kodex/.cache

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"]
CMD ["runtime-session"]

# Зафиксированные инструменты не наследуют исходники и бинарник runner:
# изменение приложения не должно сбрасывать кэш их загрузки и сборки.
FROM build AS toolchain-build

ARG GOOSE_VERSION=v3.27.1
ARG OAPI_CODEGEN_VERSION=v2.7.1
ARG PROTOC_GEN_GO_VERSION=v1.36.11
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.6.2
ARG GOLANGCI_LINT_VERSION=v2.12.2
ARG GOIMPORTS_VERSION=v0.49.0
ARG GOFUMPT_VERSION=v0.10.0
ARG STATICCHECK_VERSION=v0.7.0
ARG BUF_VERSION=v1.71.0
ARG GRPCURL_VERSION=v1.9.3
ARG YQ_VERSION=v4.53.3
ARG SQLC_VERSION=v1.31.1
ARG MOCKGEN_VERSION=v0.6.0

ENV GOBIN=/out/kodex-protected

COPY services/jobs/agent-runner/build-go-tool.sh /opt/kodex/build-go-tool.sh

RUN --mount=type=cache,id=kodex-public-go-mod,target=/go/pkg/mod,sharing=shared \
	--mount=type=cache,id=kodex-public-go-build-1.26.10,target=/root/.cache/go-build,sharing=shared \
	apk add --no-cache ca-certificates git \
	&& mkdir -p "${GOBIN}" \
	&& sh /opt/kodex/build-go-tool.sh github.com/pressly/goose/v3/cmd/goose@"${GOOSE_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@"${OAPI_CODEGEN_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh google.golang.org/protobuf/cmd/protoc-gen-go@"${PROTOC_GEN_GO_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh google.golang.org/grpc/cmd/protoc-gen-go-grpc@"${PROTOC_GEN_GO_GRPC_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh github.com/golangci/golangci-lint/v2/cmd/golangci-lint@"${GOLANGCI_LINT_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh golang.org/x/tools/cmd/goimports@"${GOIMPORTS_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh mvdan.cc/gofumpt@"${GOFUMPT_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh honnef.co/go/tools/cmd/staticcheck@"${STATICCHECK_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh github.com/bufbuild/buf/cmd/buf@"${BUF_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh github.com/fullstorydev/grpcurl/cmd/grpcurl@"${GRPCURL_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh github.com/mikefarah/yq/v4@"${YQ_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh github.com/sqlc-dev/sqlc/cmd/sqlc@"${SQLC_VERSION}" \
	&& sh /opt/kodex/build-go-tool.sh go.uber.org/mock/mockgen@"${MOCKGEN_VERSION}"
# Готовые CLI содержат собственную stdlib и не обновляются заменой runtime Go.
# Canonical upstream packages собираются независимо от runner и 13 Go tools.
FROM build AS platform-cli-build

ARG GH_CLI_VERSION=2.95.0
ARG KUBECTL_VERSION=v1.36.2
ARG HELM_VERSION=v4.2.1
ENV GOBIN=/out/kodex-platform

COPY --chmod=0555 services/jobs/agent-runner/build-go-tool.sh /usr/local/bin/kodex-build-go-tool
RUN --mount=type=cache,id=kodex-public-go-mod,target=/go/pkg/mod,sharing=shared \
	--mount=type=cache,id=kodex-public-go-build-1.26.10,target=/root/.cache/go-build,sharing=shared \
	set -eu; \
	apk add --no-cache ca-certificates git; \
	test "$(go env GOVERSION)" = "go1.26.10"; \
	mkdir -p "${GOBIN}"; \
	sh /usr/local/bin/kodex-build-go-tool github.com/cli/cli/v2/cmd/gh@"v${GH_CLI_VERSION}"; \
	sh /usr/local/bin/kodex-build-go-tool k8s.io/kubernetes/cmd/kubectl@"${KUBECTL_VERSION}"; \
	sh /usr/local/bin/kodex-build-go-tool helm.sh/helm/v4/cmd/helm@"${HELM_VERSION}"; \
	cli_version=$("${GOBIN}/gh" --version); \
	printf '%s\n' "$cli_version" | awk -v version="${GH_CLI_VERSION}" \
		'NR == 1 {valid=($1 == "gh" && $2 == "version" && $3 == version)} END {exit !valid}'; \
	client_version=$("${GOBIN}/kubectl" version --client -o json); \
	test "$(printf '%s\n' "$client_version" | sed -n 's/.*"gitVersion": "\([^"]*\)".*/\1/p')" = "${KUBECTL_VERSION}"; \
	test "$("${GOBIN}/helm" version --template '{{.Version}} {{.GoVersion}}')" = "${HELM_VERSION} go1.26.10"

FROM docker.io/library/node:24.22.1-bookworm@sha256:d862bb5d082c5ae0210ac07e8c0344cb8cf7bef6d78e9b8c50c179da923108ea AS opencode-binary

# Версия и checksum принадлежат source; private configuration не попадает в build.
RUN command -v curl \
	&& test -s /etc/ssl/certs/ca-certificates.crt \
	&& curl --fail --show-error --silent --location --proto '=https' --proto-redir '=https' \
		--connect-timeout 15 --max-time 180 \
		--output /tmp/opencode.tar.gz \
		https://github.com/anomalyco/opencode/releases/download/v1.19.5/opencode-linux-x64-baseline.tar.gz \
	&& echo '17c200fd4b2289954cc01552559f9b6d03ec157b2ddfb5eb54144f491656333d  /tmp/opencode.tar.gz' | sha256sum -c - \
	&& mkdir /out \
	&& tar -xzf /tmp/opencode.tar.gz -C /out -- opencode \
	&& chmod 0555 /out/opencode \
	&& test "$(/out/opencode --version)" = '1.19.5' \
	&& rm -f /tmp/opencode.tar.gz

FROM docker.io/library/node:24.22.1-bookworm@sha256:d862bb5d082c5ae0210ac07e8c0344cb8cf7bef6d78e9b8c50c179da923108ea AS full-runtime

ENV GOTOOLCHAIN=local

ARG JUST_VERSION=1.55.1
# Новая security-ревизия обновляет apt-слой, не затрагивая независимые Go tools.
ARG KODEX_TOOLCHAIN_SECURITY_REVISION=20261108
LABEL kodex.dev/toolchain-security-revision=${KODEX_TOOLCHAIN_SECURITY_REVISION}
ARG KODEX_AGENT_RUNNER_UID=10001
ARG KODEX_AGENT_RUNNER_GID=10001

COPY --from=toolchain-build /out/kodex-protected/ /usr/local/bin/
COPY --from=platform-cli-build --chmod=0555 /out/kodex-platform/ /usr/local/bin/

ENV PATH=/opt/kodex/bootstrap-go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin \
	PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
	PLAYWRIGHT_MCP_EXECUTABLE_PATH=/usr/lib/chromium/chromium

COPY --from=build /usr/local/go/ /opt/kodex/bootstrap-go/
COPY services/jobs/agent-runner/npm-toolchain/package.json services/jobs/agent-runner/npm-toolchain/package-lock.json services/jobs/agent-runner/npm-toolchain/install.mjs services/jobs/agent-runner/npm-toolchain/npm-source.json /opt/kodex/npm-toolchain/
COPY services/jobs/agent-runner/npm-toolchain/npm-cli/package.json services/jobs/agent-runner/npm-toolchain/npm-cli/package-lock.json /opt/kodex/npm-toolchain/npm-cli/

RUN set -eu; \
	case "${KODEX_TOOLCHAIN_SECURITY_REVISION}" in ''|*[!0-9]*) exit 1 ;; esac; \
	test "${#KODEX_TOOLCHAIN_SECURITY_REVISION}" -eq 8; \
	export DEBIAN_FRONTEND=noninteractive; \
	test -f /etc/apt/sources.list.d/debian.sources; \
	sed -i 's#^URIs: http://deb.debian.org/#URIs: https://deb.debian.org/#' /etc/apt/sources.list.d/debian.sources; \
	grep -Fqx 'URIs: https://deb.debian.org/debian' /etc/apt/sources.list.d/debian.sources; \
	grep -Fqx 'URIs: https://deb.debian.org/debian-security' /etc/apt/sources.list.d/debian.sources; \
	if grep -Eq '^URIs: http://' /etc/apt/sources.list.d/debian.sources; then exit 1; elif [ "$?" -gt 1 ]; then exit 1; fi; \
	apt-get -o APT::Update::Error-Mode=any update; \
	apt-get upgrade -y --no-install-recommends; \
	apt-get install -y --no-install-recommends \
		bash \
		bubblewrap \
		build-essential \
		ca-certificates \
		chromium \
		curl \
		dnsutils \
		fd-find \
		git \
		jq \
		less \
		libpcre2-8-0 \
		make \
		netcat-openbsd \
		openssl \
		openssh-client \
		postgresql-client \
		protobuf-compiler \
		redis-tools \
		ripgrep \
		tar \
		tini \
		tree; \
	require_security_version() { \
		installed_version=$(dpkg-query -W -f='${Version}' "$1"); \
		dpkg --validate-version "$installed_version"; \
		dpkg --compare-versions "$installed_version" ge "$2"; \
	}; \
	require_security_version libexpat1 '2.5.0-1+deb12u4'; \
	require_security_version libexpat1-dev '2.5.0-1+deb12u4'; \
	require_security_version libaprutil1 '1.6.3-1+deb12u1'; \
	require_security_version chromium '154.0.8037.92-1~deb12u1'; \
	ln -sf /usr/bin/tini /sbin/tini; \
	ln -sf /usr/bin/fdfind /usr/local/bin/fd; \
	ln -sf /usr/bin/chromium /usr/local/bin/chromium; \
	ln -sf /usr/local/go/bin/go /usr/local/bin/go; \
	ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt; \
	case "$(dpkg --print-architecture)" in \
		x86_64) just_arch=x86_64 ;; \
		amd64) just_arch=x86_64 ;; \
		arm64) just_arch=aarch64 ;; \
		aarch64) just_arch=aarch64 ;; \
		*) echo "unsupported architecture: $(dpkg --print-architecture)" >&2; exit 1 ;; \
	esac; \
	curl_flags="--ipv4 --fail --show-error --silent --location --retry 5 --retry-all-errors --connect-timeout 20 --max-time 300"; \
	curl ${curl_flags} -o "/tmp/just.tar.gz" "https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-${just_arch}-unknown-linux-musl.tar.gz"; \
	tar -xzf /tmp/just.tar.gz -C /tmp just; \
	mv /tmp/just /usr/local/bin/just; \
	chmod +x /usr/local/bin/gh /usr/local/bin/kubectl /usr/local/bin/helm /usr/local/bin/just; \
	rm -rf /tmp/just.tar.gz; \
	node /opt/kodex/npm-toolchain/install.mjs prepare-npm; \
	npm ci --prefix /opt/kodex/npm-toolchain --ignore-scripts --no-audit --no-fund --registry=https://registry.npmjs.org/; \
	npm ci --prefix /opt/kodex/npm-toolchain/npm-cli --ignore-scripts --no-audit --no-fund --registry=https://registry.npmjs.org/; \
	node /opt/kodex/npm-toolchain/install.mjs verify; \
	node /opt/kodex/npm-toolchain/npm-cli/bin/npm-cli.js uninstall --global npm --prefix /usr/local --ignore-scripts --no-audit --no-fund; \
	test ! -e /usr/local/lib/node_modules/npm; \
	node /opt/kodex/npm-toolchain/install.mjs publish-links; \
	test -x "${PLAYWRIGHT_MCP_EXECUTABLE_PATH}"; \
	playwright-mcp --help | grep -F -- '--executable-path' >/dev/null; \
	node --version; \
	npm --version; \
	go version; \
	gh --version >/dev/null; \
	kubectl version --client=true >/dev/null; \
	helm version --short >/dev/null; \
	psql --version >/dev/null; \
	redis-cli --version >/dev/null; \
	jq --version >/dev/null; \
	yq --version >/dev/null; \
	rg --version >/dev/null; \
	fd --version >/dev/null; \
	just --version >/dev/null; \
	goose --version >/dev/null; \
	oapi-codegen -version >/dev/null; \
	protoc --version >/dev/null; \
	protoc-gen-go --version >/dev/null; \
	protoc-gen-go-grpc --version >/dev/null; \
	command -v goimports >/dev/null; \
	command -v gofumpt >/dev/null; \
	staticcheck -version >/dev/null; \
	buf --version >/dev/null; \
	grpcurl --version >/dev/null; \
	sqlc version >/dev/null; \
	mockgen -version >/dev/null; \
	golangci-lint version >/dev/null; \
	tsc --version >/dev/null; \
	command -v create-vue >/dev/null; \
	vite --version >/dev/null; \
	vue-tsc --version >/dev/null; \
	vitest --version >/dev/null; \
	eslint --version >/dev/null; \
	prettier --version >/dev/null; \
	pnpm --version >/dev/null; \
	yarn --version >/dev/null; \
	command -v openapi-ts >/dev/null; \
	command -v wscat >/dev/null; \
	chromium --version >/dev/null; \
	playwright --version >/dev/null; \
	command -v playwright-mcp >/dev/null; \
	wait-on --version >/dev/null; \
	codex --version >/dev/null; \
	codex-code-mode-host --help >/dev/null; \
	npm cache clean --force; \
	rm -rf /var/lib/apt/lists/* /root/.cache

# Относительные ссылки остаются внутри final rootfs при чтении через os.Root.
RUN test "$(yarn --version)" = "$(node -p 'require("/opt/kodex/npm-toolchain/package.json").dependencies.yarn')"

RUN groupadd --gid "${KODEX_AGENT_RUNNER_GID}" kodex \
	&& useradd --uid "${KODEX_AGENT_RUNNER_UID}" --gid "${KODEX_AGENT_RUNNER_GID}" --no-create-home --shell /usr/sbin/nologin kodex \
	&& mkdir -p /home/kodex/.npm /home/kodex/.config /home/kodex/.cache /workspace /codex-home /run/secrets/kodex/runtime/provider \
	&& touch /run/secrets/kodex/runtime/provider/auth.json /run/secrets/kodex/runtime/provider/auth.sha256 \
	&& chmod 0755 /run/secrets/kodex/runtime/provider \
	&& chmod 0400 /run/secrets/kodex/runtime/provider/auth.json /run/secrets/kodex/runtime/provider/auth.sha256 \
	&& chown -R "${KODEX_AGENT_RUNNER_UID}:${KODEX_AGENT_RUNNER_GID}" /home/kodex /workspace /codex-home

ENV HOME=/home/kodex \
	NPM_CONFIG_CACHE=/home/kodex/.npm \
	XDG_CONFIG_HOME=/home/kodex/.config \
	XDG_CACHE_HOME=/home/kodex/.cache

# Playwright использует штатный executablePath без второго устаревшего браузера.
# Probe запускается с той же непривилегированной идентичностью, что и помощник.
RUN runuser -u kodex -- node -e 'const {chromium}=require("/opt/kodex/npm-toolchain/node_modules/playwright"); (async()=>{const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_MCP_EXECUTABLE_PATH,headless:true});try{const page=await browser.newPage();await page.setContent("<title>kodex</title>");if(await page.title()!=="kodex")throw new Error("System Chromium probe failed");}finally{await browser.close();}})().catch(()=>{console.error("System Chromium probe failed");process.exit(1);});'

COPY --chmod=0555 --from=build /out/kodex-go-toolchain-guard /kodex-go-toolchain-guard
RUN ["/kodex-go-toolchain-guard", "prepare", "services"]
COPY --from=build /usr/local/go/ /usr/local/go/
COPY --from=toolchain-build /out/kodex-protected/ /opt/kodex/protected-artifacts/
COPY --from=runner-build /out/kodex-protected/kodex-agent-runner /opt/kodex/protected-artifacts/kodex-agent-runner
COPY --from=build /out/kodex-go-toolchain-guard /opt/kodex/protected-artifacts/kodex-init
ENV GOROOT=/usr/local/go \
	GOENV=off \
	GOFLAGS= \
	GOTOOLCHAIN=local \
	PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin
RUN ["/kodex-go-toolchain-guard", "install", "services", "/usr/local/go/bin/go"]

# Публичные manifest/lock не наследуют private mode исходного build context.
# Проверяем настоящий npm той же non-root идентичностью, не меняя probe sandbox.
RUN chmod 0444 /opt/kodex/npm-toolchain/package.json /opt/kodex/npm-toolchain/package-lock.json \
	/opt/kodex/npm-toolchain/npm-cli/package.json /opt/kodex/npm-toolchain/npm-cli/package-lock.json \
	&& test "$(runuser -u kodex -- env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=/nonexistent \
		NPM_CONFIG_LOGS_MAX=0 NPM_CONFIG_UPDATE_NOTIFIER=false npm --version)" = \
		"$(node -p 'require("/opt/kodex/npm-toolchain/npm-cli/package.json").version')"

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"]

# Выбирается отдельно по exact target; обычный образ сохраняет прежний набор binaries.
FROM full-runtime AS opencode-runtime
COPY --from=opencode-binary --chown=0:0 --chmod=0555 /out/opencode /usr/local/bin/opencode
RUN OPENCODE_DISABLE_MODELS_FETCH=true OPENCODE_DISABLE_AUTOUPDATE=true \
	test "$(OPENCODE_DISABLE_MODELS_FETCH=true OPENCODE_DISABLE_AUTOUPDATE=true opencode --version)" = '1.19.5'

FROM full-runtime AS default-runtime
