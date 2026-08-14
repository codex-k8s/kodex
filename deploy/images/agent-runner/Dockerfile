FROM golang:1.26.5-alpine AS scratch

RUN mkdir -p /out /tmp/mattercodex-go-build-cache \
  && printf '%s' 'cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImNyeXB0by9zaGEyNTYiCgkiZm10IgoJImlvIgoJIm9zIgoJIm9zL2V4ZWMiCgkib3Mvc2lnbmFsIgoJInBhdGgvZmlsZXBhdGgiCgkic29ydCIKCSJzdHJpbmdzIgoJInN5c2NhbGwiCikKCmNvbnN0ICgKCXRydXN0ZWRHT1JPT1QgICAgICAgPSAiL3Vzci9sb2NhbC9nbyIKCWJvb3RzdHJhcEdPUk9PVCAgICAgPSAiL29wdC9tYXR0ZXJjb2RleC9ib290c3RyYXAtZ28iCglwcm90ZWN0ZWRTdGFnaW5nICAgID0gIi9vcHQvbWF0dGVyY29kZXgvcHJvdGVjdGVkLWFydGlmYWN0cyIKCXByb3RlY3RlZFRhcmdldERpciAgPSAiL3Vzci9sb2NhbC9iaW4iCglwcm90ZWN0ZWRSb290ICAgICAgID0gIi8iCgl0cnVzdGVkVUlEICAgICAgICAgID0gMAoJdHJ1c3RlZEdJRCAgICAgICAgICA9IDAKCXByU2V0Q2hpbGRTdWJyZWFwZXIgPSAzNgoJbWF4TGludXhTaWduYWwgICAgICA9IDY0CglnbGliY1NpZ25hbENhbmNlbCAgID0gMzIKCWdsaWJjU2lnbmFsU2V0WElEICAgPSAzMwopCgp2YXIgcHJvdGVjdGVkVG9vbHMgPSBbXXN0cmluZ3sKCSJidWYiLAoJImdvZnVtcHQiLAoJImdvaW1wb3J0cyIsCgkiZ29sYW5nY2ktbGludCIsCgkiZ29vc2UiLAoJImdycGN1cmwiLAoJIm1vY2tnZW4iLAoJIm9hcGktY29kZWdlbiIsCgkicHJvdG9jLWdlbi1nbyIsCgkicHJvdG9jLWdlbi1nby1ncnBjIiwKCSJzcWxjIiwKCSJzdGF0aWNjaGVjayIsCgkieXEiLAp9CgpmdW5jIGZhaWwoZm9ybWF0IHN0cmluZywgdmFsdWVzIC4uLmFueSkgewoJZm10LkZwcmludGYob3MuU3RkZXJyLCBmb3JtYXQrIlxuIiwgdmFsdWVzLi4uKQoJb3MuRXhpdCgxKQp9CgpmdW5jIGJvb3RzdHJhcEd1YXJkUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRSb290LCAibWF0dGVyY29kZXgtZ28tdG9vbGNoYWluLWd1YXJkIikKfQoKZnVuYyBzZXJ2aWNlc0VudHJ5cG9pbnRQYXRoKCkgc3RyaW5nIHsKCXJldHVybiBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgIm1hdHRlcmNvZGV4LWluaXQiKQp9CgpmdW5jIHNlcnZpY2VzUnVubmVyUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsICJtYXR0ZXItY29kZXgtYWdlbnQtcnVubmVyIikKfQoKZnVuYyB0cnVzdGVkR29Db21tYW5kKGdvRXhlY3V0YWJsZSBzdHJpbmcsIGFyZ3VtZW50cyAuLi5zdHJpbmcpICpleGVjLkNtZCB7Cgljb21tYW5kIDo9IGV4ZWMuQ29tbWFuZChnb0V4ZWN1dGFibGUsIGFyZ3VtZW50cy4uLikKCWNvbW1hbmQuRW52ID0gW11zdHJpbmd7CgkJIlBBVEg9L3Vzci9sb2NhbC9nby9iaW46L3Vzci9iaW46L2JpbiIsCgkJIkhPTUU9L3RtcCIsCgkJIkdPUk9PVD0iICsgdHJ1c3RlZEdPUk9PVCwKCQkiR09FTlY9b2ZmIiwKCQkiR09GTEFHUz0iLAoJCSJHT1RPT0xDSEFJTj1sb2NhbCIsCgkJIkdPV09SSz1vZmYiLAoJfQoJcmV0dXJuIGNvbW1hbmQKfQoKZnVuYyBleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCBsYWJlbCwgZXhwZWN0ZWQgc3RyaW5nLCBhcmd1bWVudHMgLi4uc3RyaW5nKSB7CglvdXRwdXQsIGVyciA6PSB0cnVzdGVkR29Db21tYW5kKGdvRXhlY3V0YWJsZSwgYXJndW1lbnRzLi4uKS5PdXRwdXQoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgiJXMgZmFpbGVkOiAldiIsIGxhYmVsLCBlcnIpCgl9CglpZiAhYnl0ZXMuRXF1YWwob3V0cHV0LCBbXWJ5dGUoZXhwZWN0ZWQpKSB7CgkJZmFpbCgiJXMgbWlzbWF0Y2g6IGdvdCAlcSwgd2FudCAlcSIsIGxhYmVsLCBvdXRwdXQsIGV4cGVjdGVkKQoJfQp9CgpmdW5jIHZlcmlmeUdvKGdvRXhlY3V0YWJsZSBzdHJpbmcpIHsKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1ZFUlNJT04iLCAiZ28xLjI2LjVcbiIsICJlbnYiLCAiR09WRVJTSU9OIikKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1RPT0xDSEFJTiIsICJsb2NhbFxuIiwgImVudiIsICJHT1RPT0xDSEFJTiIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09ST09UIiwgdHJ1c3RlZEdPUk9PVCsiXG4iLCAiZW52IiwgIkdPUk9PVCIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiZ28gdG9vbCBjb21waWxlIiwgImNvbXBpbGUgdmVyc2lvbiBnbzEuMjYuNVxuIiwgInRvb2wiLCAiY29tcGlsZSIsICItVj1mdWxsIikKfQoKZnVuYyBwcm90ZWN0ZWROYW1lcyhwcm9maWxlIHN0cmluZykgW11zdHJpbmcgewoJbmFtZXMgOj0gYXBwZW5kKFtdc3RyaW5nKG5pbCksIHByb3RlY3RlZFRvb2xzLi4uKQoJc3dpdGNoIHByb2ZpbGUgewoJY2FzZSAic2VydmljZXMiOgoJCW5hbWVzID0gYXBwZW5kKG5hbWVzLCAibWF0dGVyLWNvZGV4LWFnZW50LXJ1bm5lciIsICJtYXR0ZXJjb2RleC1pbml0IikKCWNhc2UgImRlcGxveSI6CgkJbmFtZXMgPSBhcHBlbmQobmFtZXMsICJidXN5Ym94IikKCWRlZmF1bHQ6CgkJZmFpbCgidW5zdXBwb3J0ZWQgcHJvZmlsZSAlcSIsIHByb2ZpbGUpCgl9Cglzb3J0LlN0cmluZ3MobmFtZXMpCglyZXR1cm4gbmFtZXMKfQoKZnVuYyByZXF1aXJlVHJ1c3RlZElkZW50aXR5KCkgewoJaWYgb3MuR2V0ZXVpZCgpICE9IHRydXN0ZWRVSUQgfHwgb3MuR2V0ZWdpZCgpICE9IHRydXN0ZWRHSUQgewoJCWZhaWwoInRydXN0ZWQgZ3VhcmQgcmVxdWlyZXMgdWlkOmdpZCAlZDolZCIsIHRydXN0ZWRVSUQsIHRydXN0ZWRHSUQpCgl9Cn0KCmZ1bmMgc3RhdChwYXRoIHN0cmluZykgKG9zLkZpbGVJbmZvLCAqc3lzY2FsbC5TdGF0X3QpIHsKCWluZm8sIGVyciA6PSBvcy5Mc3RhdChwYXRoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgibHN0YXQgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJc3RhdHVzLCBvayA6PSBpbmZvLlN5cygpLigqc3lzY2FsbC5TdGF0X3QpCglpZiAhb2sgewoJCWZhaWwoInN0YXQgJXM6IHVuc3VwcG9ydGVkIHBsYXRmb3JtIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyBlbnN1cmVEaXJlY3RvcnkocGF0aCBzdHJpbmcpIHsKCXBhdGggPSBmaWxlcGF0aC5DbGVhbihwYXRoKQoJcm9vdCA6PSBmaWxlcGF0aC5DbGVhbihwcm90ZWN0ZWRSb290KQoJaWYgcGF0aCA9PSByb290IHsKCQl2ZXJpZnlEaXJlY3RvcnkocGF0aCkKCQlyZXR1cm4KCX0KCWlmIHJvb3QgIT0gIi8iICYmICFzdHJpbmdzLkhhc1ByZWZpeChwYXRoLCByb290K3N0cmluZyhvcy5QYXRoU2VwYXJhdG9yKSkgewoJCWZhaWwoInByb3RlY3RlZCBwYXRoICVzIGVzY2FwZXMgcm9vdCAlcyIsIHBhdGgsIHJvb3QpCgl9CglwYXJlbnQgOj0gZmlsZXBhdGguRGlyKHBhdGgpCgllbnN1cmVEaXJlY3RvcnkocGFyZW50KQoJaW5mbywgZXJyIDo9IG9zLkxzdGF0KHBhdGgpCglpZiBlcnIgPT0gbmlsICYmICFpbmZvLklzRGlyKCkgewoJCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocGF0aCk7IGVyciAhPSBuaWwgewoJCQlmYWlsKCJyZW1vdmUgdW5zYWZlIHBhdGggJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCQlpbmZvID0gbmlsCgl9IGVsc2UgaWYgZXJyICE9IG5pbCAmJiAhb3MuSXNOb3RFeGlzdChlcnIpIHsKCQlmYWlsKCJsc3RhdCAlczogJXYiLCBwYXRoLCBlcnIpCgl9CglpZiBpbmZvID09IG5pbCB7CgkJaWYgZXJyIDo9IG9zLk1rZGlyKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCAmJiAhb3MuSXNFeGlzdChlcnIpIHsKCQkJZmFpbCgibWtkaXIgJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCX0KCWlmIGVyciA6PSBvcy5DaG93bihwYXRoLCB0cnVzdGVkVUlELCB0cnVzdGVkR0lEKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2hvd24gJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLkNobW9kKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2htb2QgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJdmVyaWZ5RGlyZWN0b3J5KHBhdGgpCn0KCmZ1bmMgdmVyaWZ5RGlyZWN0b3J5KHBhdGggc3RyaW5nKSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uSXNEaXIoKSB8fCBpbmZvLk1vZGUoKSZvcy5Nb2RlU3ltbGluayAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCBhIHJlYWwgZGlyZWN0b3J5IiwgcGF0aCkKCX0KCWlmIHN0YXR1cy5VaWQgIT0gdWludDMyKHRydXN0ZWRVSUQpIHx8IHN0YXR1cy5HaWQgIT0gdWludDMyKHRydXN0ZWRHSUQpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCByb290LW93bmVkIiwgcGF0aCkKCX0KCWlmIGluZm8uTW9kZSgpLlBlcm0oKSYwbzAyMiAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KfQoKZnVuYyB2ZXJpZnlQYXJlbnRzKHBhdGggc3RyaW5nKSB7Cglyb290IDo9IGZpbGVwYXRoLkNsZWFuKHByb3RlY3RlZFJvb3QpCglmb3IgY3VycmVudCA6PSBmaWxlcGF0aC5DbGVhbihwYXRoKTsgOyBjdXJyZW50ID0gZmlsZXBhdGguRGlyKGN1cnJlbnQpIHsKCQl2ZXJpZnlEaXJlY3RvcnkoY3VycmVudCkKCQlpZiBjdXJyZW50ID09IHJvb3QgewoJCQlyZXR1cm4KCQl9Cgl9Cn0KCmZ1bmMgcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUgc3RyaW5nKSB7Cglmb3IgXywgbmFtZSA6PSByYW5nZSBwcm90ZWN0ZWROYW1lcyhwcm9maWxlKSB7CgkJcGF0aCA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgbmFtZSkKCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHBhdGgpOyBlcnIgIT0gbmlsIHsKCQkJZmFpbCgicmVtb3ZlIHByb3RlY3RlZCBkZXN0aW5hdGlvbiAlczogJXYiLCBwYXRoLCBlcnIpCgkJfQoJfQp9CgpmdW5jIHByZXBhcmUocHJvZmlsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJdmVyaWZ5Qm9vdHN0cmFwR3VhcmQoKQoJZW5zdXJlRGlyZWN0b3J5KGZpbGVwYXRoLkRpcih0cnVzdGVkR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIoYm9vdHN0cmFwR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIocHJvdGVjdGVkU3RhZ2luZykpCgllbnN1cmVEaXJlY3RvcnkocHJvdGVjdGVkVGFyZ2V0RGlyKQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbCh0cnVzdGVkR09ST09UKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xlYW4gJXM6ICV2IiwgdHJ1c3RlZEdPUk9PVCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbChib290c3RyYXBHT1JPT1QpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbGVhbiAlczogJXYiLCBib290c3RyYXBHT1JPT1QsIGVycikKCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocHJvdGVjdGVkU3RhZ2luZyk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNsZWFuICVzOiAldiIsIHByb3RlY3RlZFN0YWdpbmcsIGVycikKCX0KCWVuc3VyZURpcmVjdG9yeSh0cnVzdGVkR09ST09UKQoJZW5zdXJlRGlyZWN0b3J5KHByb3RlY3RlZFN0YWdpbmcpCglyZW1vdmVQcm90ZWN0ZWRMZWF2ZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHModHJ1c3RlZEdPUk9PVCkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQp9CgpmdW5jIHZlcmlmeVJlZ3VsYXIocGF0aCBzdHJpbmcpIChvcy5GaWxlSW5mbywgKnN5c2NhbGwuU3RhdF90KSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uTW9kZSgpLklzUmVndWxhcigpIHx8IGluZm8uTW9kZSgpJm9zLk1vZGVTeW1saW5rICE9IDAgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3QgYSByZWd1bGFyIG5vbi1zeW1saW5rIGZpbGUiLCBwYXRoKQoJfQoJaWYgc3RhdHVzLlVpZCAhPSB1aW50MzIodHJ1c3RlZFVJRCkgfHwgc3RhdHVzLkdpZCAhPSB1aW50MzIodHJ1c3RlZEdJRCkgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3Qgcm9vdC1vd25lZCIsIHBhdGgpCgl9CglpZiBzdGF0dXMuTmxpbmsgIT0gMSB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGhhcyAlZCBoYXJkbGlua3MiLCBwYXRoLCBzdGF0dXMuTmxpbmspCgl9CglpZiBpbmZvLk1vZGUoKS5QZXJtKCkmMG8wMjIgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyB2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHBhdGggc3RyaW5nKSB7CglpbmZvLCBfIDo9IHZlcmlmeVJlZ3VsYXIocGF0aCkKCWlmIGluZm8uTW9kZSgpLlBlcm0oKSAhPSAwbzU1NSB7CgkJZmFpbCgicHJvdGVjdGVkIGV4ZWN1dGFibGUgJXMgaGFzIG1vZGUgJTA0bywgd2FudCAwNTU1IiwgcGF0aCwgaW5mby5Nb2RlKCkuUGVybSgpKQoJfQoJdmVyaWZ5UGFyZW50cyhmaWxlcGF0aC5EaXIocGF0aCkpCn0KCmZ1bmMgZGlnZXN0KHBhdGggc3RyaW5nKSBbc2hhMjU2LlNpemVdYnl0ZSB7CglmaWxlLCBlcnIgOj0gb3MuT3BlbihwYXRoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgib3BlbiAlczogJXYiLCBwYXRoLCBlcnIpCgl9CglkZWZlciBmaWxlLkNsb3NlKCkKCWhhc2ggOj0gc2hhMjU2Lk5ldygpCglpZiBfLCBlcnIgOj0gaW8uQ29weShoYXNoLCBmaWxlKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiaGFzaCAlczogJXYiLCBwYXRoLCBlcnIpCgl9Cgl2YXIgcmVzdWx0IFtzaGEyNTYuU2l6ZV1ieXRlCgljb3B5KHJlc3VsdFs6XSwgaGFzaC5TdW0obmlsKSkKCXJldHVybiByZXN1bHQKfQoKZnVuYyB2ZXJpZnlCb290c3RyYXBHdWFyZCgpIHsKCXBhdGggOj0gYm9vdHN0cmFwR3VhcmRQYXRoKCkKCXZlcmlmeUluc3RhbGxlZEV4ZWN1dGFibGUocGF0aCkKCWV4ZWN1dGFibGUsIGVyciA6PSBvcy5FeGVjdXRhYmxlKCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoInJlc29sdmUgcnVubmluZyBndWFyZDogJXYiLCBlcnIpCgl9CglpZiBkaWdlc3QocGF0aCkgIT0gZGlnZXN0KGV4ZWN1dGFibGUpIHsKCQlmYWlsKCJydW5uaW5nIGd1YXJkIGRpZ2VzdCBkaWZmZXJzIGZyb20gcHJvdGVjdGVkIGJvb3RzdHJhcCBndWFyZCIpCgl9Cn0KCmZ1bmMgY29weVByb3RlY3RlZChzb3VyY2UsIHRhcmdldCBzdHJpbmcpIHsKCXNvdXJjZUZpbGUsIGVyciA6PSBvcy5PcGVuKHNvdXJjZSkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoIm9wZW4gcHJvdGVjdGVkIHNvdXJjZSAlczogJXYiLCBzb3VyY2UsIGVycikKCX0KCWRlZmVyIHNvdXJjZUZpbGUuQ2xvc2UoKQoJdGFyZ2V0RmlsZSwgZXJyIDo9IG9zLk9wZW5GaWxlKHRhcmdldCwgb3MuT19XUk9OTFl8b3MuT19DUkVBVEV8b3MuT19FWENMLCAwbzU1NSkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoImNyZWF0ZSBwcm90ZWN0ZWQgZGVzdGluYXRpb24gJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9CglpZiBfLCBlcnIgOj0gaW8uQ29weSh0YXJnZXRGaWxlLCBzb3VyY2VGaWxlKTsgZXJyICE9IG5pbCB7CgkJdGFyZ2V0RmlsZS5DbG9zZSgpCgkJZmFpbCgiY29weSBwcm90ZWN0ZWQgYXJ0aWZhY3QgJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9CglpZiBlcnIgOj0gdGFyZ2V0RmlsZS5TeW5jKCk7IGVyciAhPSBuaWwgewoJCXRhcmdldEZpbGUuQ2xvc2UoKQoJCWZhaWwoInN5bmMgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuQ2htb2QoMG81NTUpOyBlcnIgIT0gbmlsIHsKCQl0YXJnZXRGaWxlLkNsb3NlKCkKCQlmYWlsKCJjaG1vZCBwcm90ZWN0ZWQgYXJ0aWZhY3QgJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9CglpZiBlcnIgOj0gdGFyZ2V0RmlsZS5DbG9zZSgpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbG9zZSBwcm90ZWN0ZWQgYXJ0aWZhY3QgJXM6ICV2IiwgdGFyZ2V0LCBlcnIpCgl9Cn0KCmZ1bmMgdmVyaWZ5RXhhY3RTdGFnaW5nKG5hbWVzIFtdc3RyaW5nKSB7CgllbnRyaWVzLCBlcnIgOj0gb3MuUmVhZERpcihwcm90ZWN0ZWRTdGFnaW5nKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgicmVhZCBwcm90ZWN0ZWQgc3RhZ2luZzogJXYiLCBlcnIpCgl9CglpZiBsZW4oZW50cmllcykgIT0gbGVuKG5hbWVzKSB7CgkJZmFpbCgicHJvdGVjdGVkIHN0YWdpbmcgaGFzICVkIGFydGlmYWN0cywgd2FudCAlZCIsIGxlbihlbnRyaWVzKSwgbGVuKG5hbWVzKSkKCX0KCWZvciBpbmRleCwgZW50cnkgOj0gcmFuZ2UgZW50cmllcyB7CgkJaWYgZW50cnkuTmFtZSgpICE9IG5hbWVzW2luZGV4XSB7CgkJCWZhaWwoInByb3RlY3RlZCBzdGFnaW5nIGFydGlmYWN0ICVxLCB3YW50ICVxIiwgZW50cnkuTmFtZSgpLCBuYW1lc1tpbmRleF0pCgkJfQoJfQp9CgpmdW5jIGluc3RhbGwocHJvZmlsZSwgZ29FeGVjdXRhYmxlIHN0cmluZykgewoJcmVxdWlyZVRydXN0ZWRJZGVudGl0eSgpCgl2ZXJpZnlCb290c3RyYXBHdWFyZCgpCgluYW1lcyA6PSBwcm90ZWN0ZWROYW1lcyhwcm9maWxlKQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRTdGFnaW5nKQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRUYXJnZXREaXIpCgl2ZXJpZnlQYXJlbnRzKHRydXN0ZWRHT1JPT1QpCgl2ZXJpZnlFeGFjdFN0YWdpbmcobmFtZXMpCglzb3VyY2VEaWdlc3RzIDo9IG1ha2UobWFwW3N0cmluZ11bc2hhMjU2LlNpemVdYnl0ZSwgbGVuKG5hbWVzKSkKCWZvciBfLCBuYW1lIDo9IHJhbmdlIG5hbWVzIHsKCQlzb3VyY2UgOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRTdGFnaW5nLCBuYW1lKQoJCXZlcmlmeVJlZ3VsYXIoc291cmNlKQoJCXNvdXJjZURpZ2VzdHNbbmFtZV0gPSBkaWdlc3Qoc291cmNlKQoJfQoJcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJc291cmNlIDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkU3RhZ2luZywgbmFtZSkKCQl0YXJnZXQgOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsIG5hbWUpCgkJY29weVByb3RlY3RlZChzb3VyY2UsIHRhcmdldCkKCQl2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHRhcmdldCkKCQlpZiBkaWdlc3QodGFyZ2V0KSAhPSBzb3VyY2VEaWdlc3RzW25hbWVdIHsKCQkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0IGRpZ2VzdCBtaXNtYXRjaCBmb3IgJXMiLCBuYW1lKQoJCX0KCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocHJvdGVjdGVkU3RhZ2luZyk7IGVyciAhPSBuaWwgewoJCWZhaWwoInJlbW92ZSBwcm90ZWN0ZWQgc3RhZ2luZzogJXYiLCBlcnIpCgl9Cgl2ZXJpZnlQYXJlbnRzKHByb3RlY3RlZFRhcmdldERpcikKCWZvciBfLCBuYW1lIDo9IHJhbmdlIG5hbWVzIHsKCQl0YXJnZXQgOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsIG5hbWUpCgkJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZSh0YXJnZXQpCgkJaWYgZGlnZXN0KHRhcmdldCkgIT0gc291cmNlRGlnZXN0c1tuYW1lXSB7CgkJCWZhaWwoImZpbmFsIHByb3RlY3RlZCBhcnRpZmFjdCBkaWdlc3QgbWlzbWF0Y2ggZm9yICVzIiwgbmFtZSkKCQl9Cgl9Cgl2ZXJpZnlSZWd1bGFyKGdvRXhlY3V0YWJsZSkKCXZlcmlmeUdvKGdvRXhlY3V0YWJsZSkKfQoKZnVuYyBlbmFibGVTdWJyZWFwZXIoKSB7CglfLCBfLCBlcnJubyA6PSBzeXNjYWxsLlJhd1N5c2NhbGw2KHN5c2NhbGwuU1lTX1BSQ1RMLCBwclNldENoaWxkU3VicmVhcGVyLCAxLCAwLCAwLCAwLCAwKQoJaWYgZXJybm8gIT0gMCB7CgkJZmFpbCgiZW5hYmxlIGNoaWxkIHN1YnJlYXBlcjogJXYiLCBlcnJubykKCX0KfQoKZnVuYyBwcm94eVNpZ25hbHMoKSBbXW9zLlNpZ25hbCB7CglzaWduYWxzIDo9IG1ha2UoW11vcy5TaWduYWwsIDAsIG1heExpbnV4U2lnbmFsLTcpCglmb3IgbnVtYmVyIDo9IDE7IG51bWJlciA8PSBtYXhMaW51eFNpZ25hbDsgbnVtYmVyKysgewoJCWN1cnJlbnQgOj0gc3lzY2FsbC5TaWduYWwobnVtYmVyKQoJCXN3aXRjaCBjdXJyZW50IHsKCQljYXNlIHN5c2NhbGwuU0lHS0lMTCwKCQkJc3lzY2FsbC5TSUdTVE9QLAoJCQlzeXNjYWxsLlNJR0NITEQsCgkJCXN5c2NhbGwuU0lHVVJHLAoJCQlzeXNjYWxsLlNJR1BST0YsCgkJCXN5c2NhbGwuU2lnbmFsKGdsaWJjU2lnbmFsQ2FuY2VsKSwKCQkJc3lzY2FsbC5TaWduYWwoZ2xpYmNTaWduYWxTZXRYSUQpOgoJCQljb250aW51ZQoJCWRlZmF1bHQ6CgkJCXNpZ25hbHMgPSBhcHBlbmQoc2lnbmFscywgY3VycmVudCkKCQl9Cgl9CglyZXR1cm4gc2lnbmFscwp9CgpmdW5jIGZvcndhcmRTaWduYWxzKHNpZ25hbHMgPC1jaGFuIG9zLlNpZ25hbCwgcHJvY2Vzc0dyb3VwIGludCwgc3RvcCA8LWNoYW4gc3RydWN0e30sIGRvbmUgY2hhbjwtIHN0cnVjdHt9KSB7CglkZWZlciBjbG9zZShkb25lKQoJZm9yIHsKCQlzZWxlY3QgewoJCWNhc2UgPC1zdG9wOgoJCQlyZXR1cm4KCQlkZWZhdWx0OgoJCX0KCQlzZWxlY3QgewoJCWNhc2UgcmVjZWl2ZWQgOj0gPC1zaWduYWxzOgoJCQlpZiByZWNlaXZlZCA9PSBuaWwgewoJCQkJY29udGludWUKCQkJfQoJCQlpZiB0eXBlZCwgb2sgOj0gcmVjZWl2ZWQuKHN5c2NhbGwuU2lnbmFsKTsgb2sgewoJCQkJc2VsZWN0IHsKCQkJCWNhc2UgPC1zdG9wOgoJCQkJCXJldHVybgoJCQkJZGVmYXVsdDoKCQkJCX0KCQkJCV8gPSBzeXNjYWxsLktpbGwoLXByb2Nlc3NHcm91cCwgdHlwZWQpCgkJCX0KCQljYXNlIDwtc3RvcDoKCQkJcmV0dXJuCgkJfQoJfQp9CgpmdW5jIHJlYXBFeGl0ZWRDaGlsZHJlbigpIHsKCWZvciB7CgkJdmFyIHN0YXR1cyBzeXNjYWxsLldhaXRTdGF0dXMKCQlwaWQsIGVyciA6PSBzeXNjYWxsLldhaXQ0KC0xLCAmc3RhdHVzLCBzeXNjYWxsLldOT0hBTkcsIG5pbCkKCQlzd2l0Y2ggewoJCWNhc2UgZXJyID09IHN5c2NhbGwuRUlOVFI6CgkJCWNvbnRpbnVlCgkJY2FzZSBlcnIgPT0gc3lzY2FsbC5FQ0hJTEQ6CgkJCXJldHVybgoJCWNhc2UgZXJyICE9IG5pbDoKCQkJZmFpbCgicmVhcCBleGl0ZWQgY2hpbGQ6ICV2IiwgZXJyKQoJCWNhc2UgcGlkID09IDA6CgkJCXJldHVybgoJCX0KCX0KfQoKZnVuYyBleGl0Q29kZShzdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzKSBpbnQgewoJaWYgc3RhdHVzLkV4aXRlZCgpIHsKCQlyZXR1cm4gc3RhdHVzLkV4aXRTdGF0dXMoKQoJfQoJaWYgc3RhdHVzLlNpZ25hbGVkKCkgewoJCXJldHVybiAxMjggKyBpbnQoc3RhdHVzLlNpZ25hbCgpKQoJfQoJcmV0dXJuIDEKfQoKZnVuYyBydW5TZXJ2aWNlc0VudHJ5cG9pbnQocnVubmVyIHN0cmluZywgYXJndW1lbnRzIFtdc3RyaW5nKSB7CglpZiBydW5uZXIgIT0gc2VydmljZXNSdW5uZXJQYXRoKCkgewoJCWZhaWwoInByb3RlY3RlZCBlbnRyeXBvaW50IHJlcXVpcmVzIGV4YWN0IHJ1bm5lciAlcyIsIHNlcnZpY2VzUnVubmVyUGF0aCgpKQoJfQoJaWYgZmlsZXBhdGguQ2xlYW4ob3MuQXJnc1swXSkgIT0gc2VydmljZXNFbnRyeXBvaW50UGF0aCgpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZW50cnlwb2ludCBtdXN0IHJ1biBmcm9tICVzIiwgc2VydmljZXNFbnRyeXBvaW50UGF0aCgpKQoJfQoJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZShzZXJ2aWNlc0VudHJ5cG9pbnRQYXRoKCkpCgl2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHJ1bm5lcikKCWVuYWJsZVN1YnJlYXBlcigpCgoJY29tbWFuZCA6PSBleGVjLkNvbW1hbmQocnVubmVyLCBhcmd1bWVudHMuLi4pCgljb21tYW5kLkVudiA9IG9zLkVudmlyb24oKQoJY29tbWFuZC5TdGRpbiA9IG9zLlN0ZGluCgljb21tYW5kLlN0ZG91dCA9IG9zLlN0ZG91dAoJY29tbWFuZC5TdGRlcnIgPSBvcy5TdGRlcnIKCWNvbW1hbmQuU3lzUHJvY0F0dHIgPSAmc3lzY2FsbC5TeXNQcm9jQXR0cntTZXRwZ2lkOiB0cnVlfQoKCXByb3hpZWRTaWduYWxzIDo9IHByb3h5U2lnbmFscygpCglzaWduYWxzIDo9IG1ha2UoY2hhbiBvcy5TaWduYWwsIGxlbihwcm94aWVkU2lnbmFscykqMikKCXNpZ25hbC5Ob3RpZnkoc2lnbmFscywgcHJveGllZFNpZ25hbHMuLi4pCglpZiBlcnIgOj0gY29tbWFuZC5TdGFydCgpOyBlcnIgIT0gbmlsIHsKCQlzaWduYWwuU3RvcChzaWduYWxzKQoJCWZhaWwoInN0YXJ0IHByb3RlY3RlZCBydW5uZXI6ICV2IiwgZXJyKQoJfQoKCXN0b3BGb3J3YXJkaW5nIDo9IG1ha2UoY2hhbiBzdHJ1Y3R7fSkKCWZvcndhcmRpbmdEb25lIDo9IG1ha2UoY2hhbiBzdHJ1Y3R7fSkKCWdvIGZvcndhcmRTaWduYWxzKHNpZ25hbHMsIGNvbW1hbmQuUHJvY2Vzcy5QaWQsIHN0b3BGb3J3YXJkaW5nLCBmb3J3YXJkaW5nRG9uZSkKCXZhciBydW5uZXJTdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzCglmb3IgewoJCXZhciBzdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzCgkJcGlkLCBlcnIgOj0gc3lzY2FsbC5XYWl0NCgtMSwgJnN0YXR1cywgMCwgbmlsKQoJCWlmIGVyciA9PSBzeXNjYWxsLkVJTlRSIHsKCQkJY29udGludWUKCQl9CgkJaWYgZXJyICE9IG5pbCB7CgkJCWNsb3NlKHN0b3BGb3J3YXJkaW5nKQoJCQlzaWduYWwuU3RvcChzaWduYWxzKQoJCQk8LWZvcndhcmRpbmdEb25lCgkJCWZhaWwoIndhaXQgZm9yIHByb3RlY3RlZCBydW5uZXI6ICV2IiwgZXJyKQoJCX0KCQlpZiBwaWQgPT0gY29tbWFuZC5Qcm9jZXNzLlBpZCB7CgkJCXJ1bm5lclN0YXR1cyA9IHN0YXR1cwoJCQlicmVhawoJCX0KCX0KCWNsb3NlKHN0b3BGb3J3YXJkaW5nKQoJc2lnbmFsLlN0b3Aoc2lnbmFscykKCTwtZm9yd2FyZGluZ0RvbmUKCXJlYXBFeGl0ZWRDaGlsZHJlbigpCglvcy5FeGl0KGV4aXRDb2RlKHJ1bm5lclN0YXR1cykpCn0KCmZ1bmMgbWFpbigpIHsKCWlmIGxlbihvcy5BcmdzKSA9PSAzICYmIG9zLkFyZ3NbMV0gPT0gInByZXBhcmUiIHsKCQlwcmVwYXJlKG9zLkFyZ3NbMl0pCgkJcmV0dXJuCgl9CglpZiBsZW4ob3MuQXJncykgPT0gNCAmJiBvcy5BcmdzWzFdID09ICJpbnN0YWxsIiB7CgkJaW5zdGFsbChvcy5BcmdzWzJdLCBvcy5BcmdzWzNdKQoJCXJldHVybgoJfQoJaWYgbGVuKG9zLkFyZ3MpID09IDMgJiYgb3MuQXJnc1sxXSA9PSAidmVyaWZ5IiB7CgkJdmVyaWZ5R28ob3MuQXJnc1syXSkKCQlyZXR1cm4KCX0KCWlmIGxlbihvcy5BcmdzKSA+PSAzICYmIG9zLkFyZ3NbMV0gPT0gImVudHJ5cG9pbnQiIHsKCQlydW5TZXJ2aWNlc0VudHJ5cG9pbnQob3MuQXJnc1syXSwgb3MuQXJnc1szOl0pCgkJcmV0dXJuCgl9CglmYWlsKCJ1bnN1cHBvcnRlZCBpbnZvY2F0aW9uIikKfQo=' | base64 -d > /tmp/mattercodex-go-toolchain-guard.go \
  && /usr/bin/env -i PATH=/usr/local/go/bin:/usr/bin:/bin HOME=/tmp GOCACHE=/tmp/mattercodex-go-build-cache GOENV=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -buildvcs=false -o /out/mattercodex-go-toolchain-guard /tmp/mattercodex-go-toolchain-guard.go \
  && rm -rf /tmp/mattercodex-go-toolchain-guard.go /tmp/mattercodex-go-build-cache

FROM golang:1.26.5-alpine AS context

ENV GOTOOLCHAIN=local

ARG GOOSE_VERSION=v3.27.1
ARG OAPI_CODEGEN_VERSION=v2.7.1
ARG PROTOC_GEN_GO_VERSION=v1.36.11
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.6.2
ARG GOLANGCI_LINT_VERSION=v2.12.2
ARG GOIMPORTS_VERSION=v0.46.0
ARG GOFUMPT_VERSION=v0.10.0
ARG STATICCHECK_VERSION=v0.7.0
ARG BUF_VERSION=v1.71.0
ARG GRPCURL_VERSION=v1.9.3
ARG YQ_VERSION=v4.53.3
ARG SQLC_VERSION=v1.31.1
ARG MOCKGEN_VERSION=v0.6.0

ENV GOBIN=/out/mattercodex-protected

RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN apk add --no-cache ca-certificates git \
  && mkdir -p "${GOBIN}" \
  && CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@"${GOOSE_VERSION}" \
  && CGO_ENABLED=0 go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@"${OAPI_CODEGEN_VERSION}" \
  && CGO_ENABLED=0 go install google.golang.org/protobuf/cmd/protoc-gen-go@"${PROTOC_GEN_GO_VERSION}" \
  && CGO_ENABLED=0 go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@"${PROTOC_GEN_GO_GRPC_VERSION}" \
  && CGO_ENABLED=0 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@"${GOLANGCI_LINT_VERSION}" \
  && CGO_ENABLED=0 go install golang.org/x/tools/cmd/goimports@"${GOIMPORTS_VERSION}" \
  && CGO_ENABLED=0 go install mvdan.cc/gofumpt@"${GOFUMPT_VERSION}" \
  && CGO_ENABLED=0 go install honnef.co/go/tools/cmd/staticcheck@"${STATICCHECK_VERSION}" \
  && CGO_ENABLED=0 go install github.com/bufbuild/buf/cmd/buf@"${BUF_VERSION}" \
  && CGO_ENABLED=0 go install github.com/fullstorydev/grpcurl/cmd/grpcurl@"${GRPCURL_VERSION}" \
  && CGO_ENABLED=0 go install github.com/mikefarah/yq/v4@"${YQ_VERSION}" \
  && CGO_ENABLED=0 go install github.com/sqlc-dev/sqlc/cmd/sqlc@"${SQLC_VERSION}" \
  && CGO_ENABLED=0 go install go.uber.org/mock/mockgen@"${MOCKGEN_VERSION}"
FROM node:24-alpine

ENV GOTOOLCHAIN=local

ARG CODEX_PACKAGE=@openai/codex@0.144.1
ARG GH_CLI_VERSION=2.95.0
ARG KUBECTL_VERSION=v1.36.2
ARG HELM_VERSION=v4.2.1
ARG TYPESCRIPT_VERSION=6.0.3
ARG VUE_VERSION=3.5.38
ARG CREATE_VUE_VERSION=3.22.4
ARG VITE_VERSION=8.0.16
ARG VUE_TSC_VERSION=3.3.5
ARG VITEST_VERSION=4.1.9
ARG ESLINT_VERSION=10.5.0
ARG PRETTIER_VERSION=3.8.4
ARG PNPM_VERSION=11.8.0
ARG HEY_OPENAPI_TS_VERSION=0.98.2
# AsyncAPI CLI транзитивно использует AWS SDK через oclif. Фиксируем последнюю
# полную линейку, чтобы npm не выбрал пакет с неопубликованной зависимостью.
ARG AWS_SDK_CLIENT_CLOUDFRONT_VERSION=3.1110.0
ARG AWS_SDK_CLIENT_S3_VERSION=3.1110.0
ARG AWS_SDK_CREDENTIAL_PROVIDER_NODE_VERSION=3.972.79
ARG ASYNCAPI_CLI_VERSION=6.0.2
ARG ASYNCAPI_GENERATOR_VERSION=3.3.0
ARG ASYNCAPI_MODELINA_CLI_VERSION=5.10.1
ARG WSCAT_VERSION=6.1.0
ARG MATTERCODEX_AGENT_RUNNER_UID=10001
ARG MATTERCODEX_AGENT_RUNNER_GID=10001

COPY --from=1 /out/mattercodex-protected/ /usr/local/bin/

ENV PATH=/opt/mattercodex/bootstrap-go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

COPY --from=0 /usr/local/go/ /opt/mattercodex/bootstrap-go/

RUN apk add --no-cache \
    bash \
    build-base \
    bind-tools \
    ca-certificates \
    curl \
    fd \
    git \
    jq \
    just \
    less \
    make \
    netcat-openbsd \
    openssl \
    openssh-client \
    postgresql-client \
    protobuf \
    redis \
    ripgrep \
    tar \
    tini \
    tree \
  && ln -sf /usr/local/go/bin/go /usr/local/bin/go \
  && ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt \
  && set -eu; \
  case "$(apk --print-arch)" in \
    x86_64) gh_arch=amd64; kubectl_arch=amd64; helm_arch=amd64 ;; \
    aarch64) gh_arch=arm64; kubectl_arch=arm64; helm_arch=arm64 ;; \
    *) echo "unsupported architecture: $(apk --print-arch)" >&2; exit 1 ;; \
  esac; \
  curl -fsSL -o /tmp/gh.tar.gz "https://github.com/cli/cli/releases/download/v${GH_CLI_VERSION}/gh_${GH_CLI_VERSION}_linux_${gh_arch}.tar.gz"; \
  tar -xzf /tmp/gh.tar.gz -C /tmp; \
  mv "/tmp/gh_${GH_CLI_VERSION}_linux_${gh_arch}/bin/gh" /usr/local/bin/gh; \
  curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${kubectl_arch}/kubectl"; \
  curl -fsSLo /tmp/kubectl.sha256 "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${kubectl_arch}/kubectl.sha256"; \
  echo "$(cat /tmp/kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c -; \
  curl -fsSL -o "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz" "https://get.helm.sh/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz"; \
  curl -fsSL -o "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum" "https://get.helm.sh/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum"; \
  (cd /tmp && sha256sum -c "helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum"); \
  tar -xzf "/tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz" -C /tmp; \
  mv "/tmp/linux-${helm_arch}/helm" /usr/local/bin/helm; \
  chmod +x /usr/local/bin/gh /usr/local/bin/kubectl /usr/local/bin/helm; \
  rm -rf /tmp/gh.tar.gz /tmp/gh_${GH_CLI_VERSION}_linux_${gh_arch} /tmp/kubectl.sha256 /tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz /tmp/helm-${HELM_VERSION}-linux-${helm_arch}.tar.gz.sha256sum /tmp/linux-${helm_arch} \
  && npm install -g \
    "${CODEX_PACKAGE}" \
    "typescript@${TYPESCRIPT_VERSION}" \
    "vue@${VUE_VERSION}" \
    "create-vue@${CREATE_VUE_VERSION}" \
    "vite@${VITE_VERSION}" \
    "vue-tsc@${VUE_TSC_VERSION}" \
    "vitest@${VITEST_VERSION}" \
    "eslint@${ESLINT_VERSION}" \
    "prettier@${PRETTIER_VERSION}" \
    "pnpm@${PNPM_VERSION}" \
    "@hey-api/openapi-ts@${HEY_OPENAPI_TS_VERSION}" \
    "@aws-sdk/client-cloudfront@${AWS_SDK_CLIENT_CLOUDFRONT_VERSION}" \
    "@aws-sdk/client-s3@${AWS_SDK_CLIENT_S3_VERSION}" \
    "@aws-sdk/credential-provider-node@${AWS_SDK_CREDENTIAL_PROVIDER_NODE_VERSION}" \
    "@asyncapi/cli@${ASYNCAPI_CLI_VERSION}" \
    "@asyncapi/generator@${ASYNCAPI_GENERATOR_VERSION}" \
    "@asyncapi/modelina-cli@${ASYNCAPI_MODELINA_CLI_VERSION}" \
    "wscat@${WSCAT_VERSION}" \
  && node --version \
  && npm --version \
  && go version \
  && gh --version >/dev/null \
  && kubectl version --client=true >/dev/null \
  && helm version --short >/dev/null \
  && psql --version >/dev/null \
  && redis-cli --version >/dev/null \
  && jq --version >/dev/null \
  && yq --version >/dev/null \
  && rg --version >/dev/null \
  && fd --version >/dev/null \
  && just --version >/dev/null \
  && goose --version >/dev/null \
  && oapi-codegen -version >/dev/null \
  && protoc --version >/dev/null \
  && protoc-gen-go --version >/dev/null \
  && protoc-gen-go-grpc --version >/dev/null \
  && command -v goimports >/dev/null \
  && command -v gofumpt >/dev/null \
  && staticcheck -version >/dev/null \
  && buf --version >/dev/null \
  && grpcurl --version >/dev/null \
  && sqlc version >/dev/null \
  && mockgen -version >/dev/null \
  && golangci-lint version >/dev/null \
  && tsc --version >/dev/null \
  && command -v create-vue >/dev/null \
  && vite --version >/dev/null \
  && vue-tsc --version >/dev/null \
  && vitest --version >/dev/null \
  && eslint --version >/dev/null \
  && prettier --version >/dev/null \
  && pnpm --version >/dev/null \
  && yarn --version >/dev/null \
  && command -v openapi-ts >/dev/null \
  && command -v asyncapi >/dev/null \
  && command -v modelina >/dev/null \
  && command -v wscat >/dev/null \
  && codex --version >/dev/null

RUN addgroup -g "${MATTERCODEX_AGENT_RUNNER_GID}" matter-codex \
  && adduser -D -H -u "${MATTERCODEX_AGENT_RUNNER_UID}" -G matter-codex matter-codex \
  && mkdir -p /home/matter-codex/.npm /home/matter-codex/.config /home/matter-codex/.cache /workspace /codex-home \
  && chown -R "${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}" /home/matter-codex /workspace /codex-home

ENV HOME=/home/matter-codex \
  NPM_CONFIG_CACHE=/home/matter-codex/.npm \
  XDG_CONFIG_HOME=/home/matter-codex/.config \
  XDG_CACHE_HOME=/home/matter-codex/.cache

WORKDIR /workspace

COPY --chmod=0555 --from=0 /out/mattercodex-go-toolchain-guard /mattercodex-go-toolchain-guard
RUN ["/mattercodex-go-toolchain-guard", "prepare", "deploy"]
COPY --from=0 /usr/local/go/ /usr/local/go/
COPY --from=1 /out/mattercodex-protected/ /opt/mattercodex/protected-artifacts/
COPY --from=0 /bin/busybox /opt/mattercodex/protected-artifacts/busybox
ENV GOROOT=/usr/local/go \
  GOENV=off \
  GOFLAGS= \
  GOTOOLCHAIN=local \
  PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin
RUN ["/mattercodex-go-toolchain-guard", "install", "deploy", "/usr/local/go/bin/go"]

USER 10001:10001

CMD ["/usr/local/bin/busybox", "sh"]
