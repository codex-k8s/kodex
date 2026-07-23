FROM golang:1.26.5-alpine AS scratch

RUN mkdir -p /out /tmp/mattercodex-go-build-cache \
  && printf '%s' 'cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImNyeXB0by9zaGEyNTYiCgkiZm10IgoJImlvIgoJIm9zIgoJIm9zL2V4ZWMiCgkib3Mvc2lnbmFsIgoJInBhdGgvZmlsZXBhdGgiCgkic29ydCIKCSJzdHJpbmdzIgoJInN5c2NhbGwiCikKCmNvbnN0ICgKCXRydXN0ZWRHT1JPT1QgICAgICAgPSAiL3Vzci9sb2NhbC9nbyIKCWJvb3RzdHJhcEdPUk9PVCAgICAgPSAiL29wdC9tYXR0ZXJjb2RleC9ib290c3RyYXAtZ28iCglwcm90ZWN0ZWRTdGFnaW5nICAgID0gIi9vcHQvbWF0dGVyY29kZXgvcHJvdGVjdGVkLWFydGlmYWN0cyIKCXByb3RlY3RlZFRhcmdldERpciAgPSAiL3Vzci9sb2NhbC9iaW4iCglwcm90ZWN0ZWRSb290ICAgICAgID0gIi8iCgl0cnVzdGVkVUlEICAgICAgICAgID0gMAoJdHJ1c3RlZEdJRCAgICAgICAgICA9IDAKCXByU2V0Q2hpbGRTdWJyZWFwZXIgPSAzNgopCgp2YXIgcHJvdGVjdGVkVG9vbHMgPSBbXXN0cmluZ3sKCSJidWYiLAoJImdvZnVtcHQiLAoJImdvaW1wb3J0cyIsCgkiZ29sYW5nY2ktbGludCIsCgkiZ29vc2UiLAoJImdycGN1cmwiLAoJIm1vY2tnZW4iLAoJIm9hcGktY29kZWdlbiIsCgkicHJvdG9jLWdlbi1nbyIsCgkicHJvdG9jLWdlbi1nby1ncnBjIiwKCSJzcWxjIiwKCSJzdGF0aWNjaGVjayIsCgkieXEiLAp9CgpmdW5jIGZhaWwoZm9ybWF0IHN0cmluZywgdmFsdWVzIC4uLmFueSkgewoJZm10LkZwcmludGYob3MuU3RkZXJyLCBmb3JtYXQrIlxuIiwgdmFsdWVzLi4uKQoJb3MuRXhpdCgxKQp9CgpmdW5jIGJvb3RzdHJhcEd1YXJkUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRSb290LCAibWF0dGVyY29kZXgtZ28tdG9vbGNoYWluLWd1YXJkIikKfQoKZnVuYyBzZXJ2aWNlc0VudHJ5cG9pbnRQYXRoKCkgc3RyaW5nIHsKCXJldHVybiBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgIm1hdHRlcmNvZGV4LWluaXQiKQp9CgpmdW5jIHNlcnZpY2VzUnVubmVyUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsICJtYXR0ZXItY29kZXgtYWdlbnQtcnVubmVyIikKfQoKZnVuYyB0cnVzdGVkR29Db21tYW5kKGdvRXhlY3V0YWJsZSBzdHJpbmcsIGFyZ3VtZW50cyAuLi5zdHJpbmcpICpleGVjLkNtZCB7Cgljb21tYW5kIDo9IGV4ZWMuQ29tbWFuZChnb0V4ZWN1dGFibGUsIGFyZ3VtZW50cy4uLikKCWNvbW1hbmQuRW52ID0gW11zdHJpbmd7CgkJIlBBVEg9L3Vzci9sb2NhbC9nby9iaW46L3Vzci9iaW46L2JpbiIsCgkJIkhPTUU9L3RtcCIsCgkJIkdPUk9PVD0iICsgdHJ1c3RlZEdPUk9PVCwKCQkiR09FTlY9b2ZmIiwKCQkiR09GTEFHUz0iLAoJCSJHT1RPT0xDSEFJTj1sb2NhbCIsCgkJIkdPV09SSz1vZmYiLAoJfQoJcmV0dXJuIGNvbW1hbmQKfQoKZnVuYyBleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCBsYWJlbCwgZXhwZWN0ZWQgc3RyaW5nLCBhcmd1bWVudHMgLi4uc3RyaW5nKSB7CglvdXRwdXQsIGVyciA6PSB0cnVzdGVkR29Db21tYW5kKGdvRXhlY3V0YWJsZSwgYXJndW1lbnRzLi4uKS5PdXRwdXQoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgiJXMgZmFpbGVkOiAldiIsIGxhYmVsLCBlcnIpCgl9CglpZiAhYnl0ZXMuRXF1YWwob3V0cHV0LCBbXWJ5dGUoZXhwZWN0ZWQpKSB7CgkJZmFpbCgiJXMgbWlzbWF0Y2g6IGdvdCAlcSwgd2FudCAlcSIsIGxhYmVsLCBvdXRwdXQsIGV4cGVjdGVkKQoJfQp9CgpmdW5jIHZlcmlmeUdvKGdvRXhlY3V0YWJsZSBzdHJpbmcpIHsKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1ZFUlNJT04iLCAiZ28xLjI2LjVcbiIsICJlbnYiLCAiR09WRVJTSU9OIikKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1RPT0xDSEFJTiIsICJsb2NhbFxuIiwgImVudiIsICJHT1RPT0xDSEFJTiIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09ST09UIiwgdHJ1c3RlZEdPUk9PVCsiXG4iLCAiZW52IiwgIkdPUk9PVCIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiZ28gdG9vbCBjb21waWxlIiwgImNvbXBpbGUgdmVyc2lvbiBnbzEuMjYuNVxuIiwgInRvb2wiLCAiY29tcGlsZSIsICItVj1mdWxsIikKfQoKZnVuYyBwcm90ZWN0ZWROYW1lcyhwcm9maWxlIHN0cmluZykgW11zdHJpbmcgewoJbmFtZXMgOj0gYXBwZW5kKFtdc3RyaW5nKG5pbCksIHByb3RlY3RlZFRvb2xzLi4uKQoJc3dpdGNoIHByb2ZpbGUgewoJY2FzZSAic2VydmljZXMiOgoJCW5hbWVzID0gYXBwZW5kKG5hbWVzLCAibWF0dGVyLWNvZGV4LWFnZW50LXJ1bm5lciIsICJtYXR0ZXJjb2RleC1pbml0IikKCWNhc2UgImRlcGxveSI6CgkJbmFtZXMgPSBhcHBlbmQobmFtZXMsICJtYXR0ZXJjb2RleC1zaGVsbCIpCglkZWZhdWx0OgoJCWZhaWwoInVuc3VwcG9ydGVkIHByb2ZpbGUgJXEiLCBwcm9maWxlKQoJfQoJc29ydC5TdHJpbmdzKG5hbWVzKQoJcmV0dXJuIG5hbWVzCn0KCmZ1bmMgcmVxdWlyZVRydXN0ZWRJZGVudGl0eSgpIHsKCWlmIG9zLkdldGV1aWQoKSAhPSB0cnVzdGVkVUlEIHx8IG9zLkdldGVnaWQoKSAhPSB0cnVzdGVkR0lEIHsKCQlmYWlsKCJ0cnVzdGVkIGd1YXJkIHJlcXVpcmVzIHVpZDpnaWQgJWQ6JWQiLCB0cnVzdGVkVUlELCB0cnVzdGVkR0lEKQoJfQp9CgpmdW5jIHN0YXQocGF0aCBzdHJpbmcpIChvcy5GaWxlSW5mbywgKnN5c2NhbGwuU3RhdF90KSB7CglpbmZvLCBlcnIgOj0gb3MuTHN0YXQocGF0aCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoImxzdGF0ICVzOiAldiIsIHBhdGgsIGVycikKCX0KCXN0YXR1cywgb2sgOj0gaW5mby5TeXMoKS4oKnN5c2NhbGwuU3RhdF90KQoJaWYgIW9rIHsKCQlmYWlsKCJzdGF0ICVzOiB1bnN1cHBvcnRlZCBwbGF0Zm9ybSIsIHBhdGgpCgl9CglyZXR1cm4gaW5mbywgc3RhdHVzCn0KCmZ1bmMgZW5zdXJlRGlyZWN0b3J5KHBhdGggc3RyaW5nKSB7CglwYXRoID0gZmlsZXBhdGguQ2xlYW4ocGF0aCkKCXJvb3QgOj0gZmlsZXBhdGguQ2xlYW4ocHJvdGVjdGVkUm9vdCkKCWlmIHBhdGggPT0gcm9vdCB7CgkJdmVyaWZ5RGlyZWN0b3J5KHBhdGgpCgkJcmV0dXJuCgl9CglpZiByb290ICE9ICIvIiAmJiAhc3RyaW5ncy5IYXNQcmVmaXgocGF0aCwgcm9vdCtzdHJpbmcob3MuUGF0aFNlcGFyYXRvcikpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgcGF0aCAlcyBlc2NhcGVzIHJvb3QgJXMiLCBwYXRoLCByb290KQoJfQoJcGFyZW50IDo9IGZpbGVwYXRoLkRpcihwYXRoKQoJZW5zdXJlRGlyZWN0b3J5KHBhcmVudCkKCWluZm8sIGVyciA6PSBvcy5Mc3RhdChwYXRoKQoJaWYgZXJyID09IG5pbCAmJiAhaW5mby5Jc0RpcigpIHsKCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHBhdGgpOyBlcnIgIT0gbmlsIHsKCQkJZmFpbCgicmVtb3ZlIHVuc2FmZSBwYXRoICVzOiAldiIsIHBhdGgsIGVycikKCQl9CgkJaW5mbyA9IG5pbAoJfSBlbHNlIGlmIGVyciAhPSBuaWwgJiYgIW9zLklzTm90RXhpc3QoZXJyKSB7CgkJZmFpbCgibHN0YXQgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJaWYgaW5mbyA9PSBuaWwgewoJCWlmIGVyciA6PSBvcy5Na2RpcihwYXRoLCAwbzc1NSk7IGVyciAhPSBuaWwgJiYgIW9zLklzRXhpc3QoZXJyKSB7CgkJCWZhaWwoIm1rZGlyICVzOiAldiIsIHBhdGgsIGVycikKCQl9Cgl9CglpZiBlcnIgOj0gb3MuQ2hvd24ocGF0aCwgdHJ1c3RlZFVJRCwgdHJ1c3RlZEdJRCk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNob3duICVzOiAldiIsIHBhdGgsIGVycikKCX0KCWlmIGVyciA6PSBvcy5DaG1vZChwYXRoLCAwbzc1NSk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNobW9kICVzOiAldiIsIHBhdGgsIGVycikKCX0KCXZlcmlmeURpcmVjdG9yeShwYXRoKQp9CgpmdW5jIHZlcmlmeURpcmVjdG9yeShwYXRoIHN0cmluZykgewoJaW5mbywgc3RhdHVzIDo9IHN0YXQocGF0aCkKCWlmICFpbmZvLklzRGlyKCkgfHwgaW5mby5Nb2RlKCkmb3MuTW9kZVN5bWxpbmsgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGRpcmVjdG9yeSAlcyBpcyBub3QgYSByZWFsIGRpcmVjdG9yeSIsIHBhdGgpCgl9CglpZiBzdGF0dXMuVWlkICE9IHVpbnQzMih0cnVzdGVkVUlEKSB8fCBzdGF0dXMuR2lkICE9IHVpbnQzMih0cnVzdGVkR0lEKSB7CgkJZmFpbCgicHJvdGVjdGVkIGRpcmVjdG9yeSAlcyBpcyBub3Qgcm9vdC1vd25lZCIsIHBhdGgpCgl9CglpZiBpbmZvLk1vZGUoKS5QZXJtKCkmMG8wMjIgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGRpcmVjdG9yeSAlcyBpcyBncm91cC93b3JsZC13cml0YWJsZSIsIHBhdGgpCgl9Cn0KCmZ1bmMgdmVyaWZ5UGFyZW50cyhwYXRoIHN0cmluZykgewoJcm9vdCA6PSBmaWxlcGF0aC5DbGVhbihwcm90ZWN0ZWRSb290KQoJZm9yIGN1cnJlbnQgOj0gZmlsZXBhdGguQ2xlYW4ocGF0aCk7IDsgY3VycmVudCA9IGZpbGVwYXRoLkRpcihjdXJyZW50KSB7CgkJdmVyaWZ5RGlyZWN0b3J5KGN1cnJlbnQpCgkJaWYgY3VycmVudCA9PSByb290IHsKCQkJcmV0dXJuCgkJfQoJfQp9CgpmdW5jIHJlbW92ZVByb3RlY3RlZExlYXZlcyhwcm9maWxlIHN0cmluZykgewoJZm9yIF8sIG5hbWUgOj0gcmFuZ2UgcHJvdGVjdGVkTmFtZXMocHJvZmlsZSkgewoJCXBhdGggOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsIG5hbWUpCgkJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbChwYXRoKTsgZXJyICE9IG5pbCB7CgkJCWZhaWwoInJlbW92ZSBwcm90ZWN0ZWQgZGVzdGluYXRpb24gJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCX0KfQoKZnVuYyBwcmVwYXJlKHByb2ZpbGUgc3RyaW5nKSB7CglyZXF1aXJlVHJ1c3RlZElkZW50aXR5KCkKCXZlcmlmeUJvb3RzdHJhcEd1YXJkKCkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIodHJ1c3RlZEdPUk9PVCkpCgllbnN1cmVEaXJlY3RvcnkoZmlsZXBhdGguRGlyKGJvb3RzdHJhcEdPUk9PVCkpCgllbnN1cmVEaXJlY3RvcnkoZmlsZXBhdGguRGlyKHByb3RlY3RlZFN0YWdpbmcpKQoJZW5zdXJlRGlyZWN0b3J5KHByb3RlY3RlZFRhcmdldERpcikKCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwodHJ1c3RlZEdPUk9PVCk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNsZWFuICVzOiAldiIsIHRydXN0ZWRHT1JPT1QsIGVycikKCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwoYm9vdHN0cmFwR09ST09UKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xlYW4gJXM6ICV2IiwgYm9vdHN0cmFwR09ST09ULCBlcnIpCgl9CglpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHByb3RlY3RlZFN0YWdpbmcpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbGVhbiAlczogJXYiLCBwcm90ZWN0ZWRTdGFnaW5nLCBlcnIpCgl9CgllbnN1cmVEaXJlY3RvcnkodHJ1c3RlZEdPUk9PVCkKCWVuc3VyZURpcmVjdG9yeShwcm90ZWN0ZWRTdGFnaW5nKQoJcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUpCgl2ZXJpZnlQYXJlbnRzKHRydXN0ZWRHT1JPT1QpCgl2ZXJpZnlQYXJlbnRzKHByb3RlY3RlZFN0YWdpbmcpCgl2ZXJpZnlQYXJlbnRzKHByb3RlY3RlZFRhcmdldERpcikKfQoKZnVuYyB2ZXJpZnlSZWd1bGFyKHBhdGggc3RyaW5nKSAob3MuRmlsZUluZm8sICpzeXNjYWxsLlN0YXRfdCkgewoJaW5mbywgc3RhdHVzIDo9IHN0YXQocGF0aCkKCWlmICFpbmZvLk1vZGUoKS5Jc1JlZ3VsYXIoKSB8fCBpbmZvLk1vZGUoKSZvcy5Nb2RlU3ltbGluayAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgYXJ0aWZhY3QgJXMgaXMgbm90IGEgcmVndWxhciBub24tc3ltbGluayBmaWxlIiwgcGF0aCkKCX0KCWlmIHN0YXR1cy5VaWQgIT0gdWludDMyKHRydXN0ZWRVSUQpIHx8IHN0YXR1cy5HaWQgIT0gdWludDMyKHRydXN0ZWRHSUQpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgYXJ0aWZhY3QgJXMgaXMgbm90IHJvb3Qtb3duZWQiLCBwYXRoKQoJfQoJaWYgc3RhdHVzLk5saW5rICE9IDEgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBoYXMgJWQgaGFyZGxpbmtzIiwgcGF0aCwgc3RhdHVzLk5saW5rKQoJfQoJaWYgaW5mby5Nb2RlKCkuUGVybSgpJjBvMDIyICE9IDAgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBncm91cC93b3JsZC13cml0YWJsZSIsIHBhdGgpCgl9CglyZXR1cm4gaW5mbywgc3RhdHVzCn0KCmZ1bmMgdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZShwYXRoIHN0cmluZykgewoJaW5mbywgXyA6PSB2ZXJpZnlSZWd1bGFyKHBhdGgpCglpZiBpbmZvLk1vZGUoKS5QZXJtKCkgIT0gMG81NTUgewoJCWZhaWwoInByb3RlY3RlZCBleGVjdXRhYmxlICVzIGhhcyBtb2RlICUwNG8sIHdhbnQgMDU1NSIsIHBhdGgsIGluZm8uTW9kZSgpLlBlcm0oKSkKCX0KCXZlcmlmeVBhcmVudHMoZmlsZXBhdGguRGlyKHBhdGgpKQp9CgpmdW5jIGRpZ2VzdChwYXRoIHN0cmluZykgW3NoYTI1Ni5TaXplXWJ5dGUgewoJZmlsZSwgZXJyIDo9IG9zLk9wZW4ocGF0aCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoIm9wZW4gJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJZGVmZXIgZmlsZS5DbG9zZSgpCgloYXNoIDo9IHNoYTI1Ni5OZXcoKQoJaWYgXywgZXJyIDo9IGlvLkNvcHkoaGFzaCwgZmlsZSk7IGVyciAhPSBuaWwgewoJCWZhaWwoImhhc2ggJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJdmFyIHJlc3VsdCBbc2hhMjU2LlNpemVdYnl0ZQoJY29weShyZXN1bHRbOl0sIGhhc2guU3VtKG5pbCkpCglyZXR1cm4gcmVzdWx0Cn0KCmZ1bmMgdmVyaWZ5Qm9vdHN0cmFwR3VhcmQoKSB7CglwYXRoIDo9IGJvb3RzdHJhcEd1YXJkUGF0aCgpCgl2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHBhdGgpCglleGVjdXRhYmxlLCBlcnIgOj0gb3MuRXhlY3V0YWJsZSgpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJyZXNvbHZlIHJ1bm5pbmcgZ3VhcmQ6ICV2IiwgZXJyKQoJfQoJaWYgZGlnZXN0KHBhdGgpICE9IGRpZ2VzdChleGVjdXRhYmxlKSB7CgkJZmFpbCgicnVubmluZyBndWFyZCBkaWdlc3QgZGlmZmVycyBmcm9tIHByb3RlY3RlZCBib290c3RyYXAgZ3VhcmQiKQoJfQp9CgpmdW5jIGNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQgc3RyaW5nKSB7Cglzb3VyY2VGaWxlLCBlcnIgOj0gb3MuT3Blbihzb3VyY2UpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJvcGVuIHByb3RlY3RlZCBzb3VyY2UgJXM6ICV2Iiwgc291cmNlLCBlcnIpCgl9CglkZWZlciBzb3VyY2VGaWxlLkNsb3NlKCkKCXRhcmdldEZpbGUsIGVyciA6PSBvcy5PcGVuRmlsZSh0YXJnZXQsIG9zLk9fV1JPTkxZfG9zLk9fQ1JFQVRFfG9zLk9fRVhDTCwgMG81NTUpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjcmVhdGUgcHJvdGVjdGVkIGRlc3RpbmF0aW9uICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgXywgZXJyIDo9IGlvLkNvcHkodGFyZ2V0RmlsZSwgc291cmNlRmlsZSk7IGVyciAhPSBuaWwgewoJCXRhcmdldEZpbGUuQ2xvc2UoKQoJCWZhaWwoImNvcHkgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuU3luYygpOyBlcnIgIT0gbmlsIHsKCQl0YXJnZXRGaWxlLkNsb3NlKCkKCQlmYWlsKCJzeW5jIHByb3RlY3RlZCBhcnRpZmFjdCAlczogJXYiLCB0YXJnZXQsIGVycikKCX0KCWlmIGVyciA6PSB0YXJnZXRGaWxlLkNobW9kKDBvNTU1KTsgZXJyICE9IG5pbCB7CgkJdGFyZ2V0RmlsZS5DbG9zZSgpCgkJZmFpbCgiY2htb2QgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuQ2xvc2UoKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xvc2UgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQp9CgpmdW5jIHZlcmlmeUV4YWN0U3RhZ2luZyhuYW1lcyBbXXN0cmluZykgewoJZW50cmllcywgZXJyIDo9IG9zLlJlYWREaXIocHJvdGVjdGVkU3RhZ2luZykKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoInJlYWQgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJaWYgbGVuKGVudHJpZXMpICE9IGxlbihuYW1lcykgewoJCWZhaWwoInByb3RlY3RlZCBzdGFnaW5nIGhhcyAlZCBhcnRpZmFjdHMsIHdhbnQgJWQiLCBsZW4oZW50cmllcyksIGxlbihuYW1lcykpCgl9Cglmb3IgaW5kZXgsIGVudHJ5IDo9IHJhbmdlIGVudHJpZXMgewoJCWlmIGVudHJ5Lk5hbWUoKSAhPSBuYW1lc1tpbmRleF0gewoJCQlmYWlsKCJwcm90ZWN0ZWQgc3RhZ2luZyBhcnRpZmFjdCAlcSwgd2FudCAlcSIsIGVudHJ5Lk5hbWUoKSwgbmFtZXNbaW5kZXhdKQoJCX0KCX0KfQoKZnVuYyBpbnN0YWxsKHByb2ZpbGUsIGdvRXhlY3V0YWJsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJdmVyaWZ5Qm9vdHN0cmFwR3VhcmQoKQoJbmFtZXMgOj0gcHJvdGVjdGVkTmFtZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQoJdmVyaWZ5UGFyZW50cyh0cnVzdGVkR09ST09UKQoJdmVyaWZ5RXhhY3RTdGFnaW5nKG5hbWVzKQoJc291cmNlRGlnZXN0cyA6PSBtYWtlKG1hcFtzdHJpbmddW3NoYTI1Ni5TaXplXWJ5dGUsIGxlbihuYW1lcykpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJc291cmNlIDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkU3RhZ2luZywgbmFtZSkKCQl2ZXJpZnlSZWd1bGFyKHNvdXJjZSkKCQlzb3VyY2VEaWdlc3RzW25hbWVdID0gZGlnZXN0KHNvdXJjZSkKCX0KCXJlbW92ZVByb3RlY3RlZExlYXZlcyhwcm9maWxlKQoJZm9yIF8sIG5hbWUgOj0gcmFuZ2UgbmFtZXMgewoJCXNvdXJjZSA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFN0YWdpbmcsIG5hbWUpCgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCWNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQpCgkJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZSh0YXJnZXQpCgkJaWYgZGlnZXN0KHRhcmdldCkgIT0gc291cmNlRGlnZXN0c1tuYW1lXSB7CgkJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCBkaWdlc3QgbWlzbWF0Y2ggZm9yICVzIiwgbmFtZSkKCQl9Cgl9CglpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHByb3RlY3RlZFN0YWdpbmcpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJyZW1vdmUgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRUYXJnZXREaXIpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCXZlcmlmeUluc3RhbGxlZEV4ZWN1dGFibGUodGFyZ2V0KQoJCWlmIGRpZ2VzdCh0YXJnZXQpICE9IHNvdXJjZURpZ2VzdHNbbmFtZV0gewoJCQlmYWlsKCJmaW5hbCBwcm90ZWN0ZWQgYXJ0aWZhY3QgZGlnZXN0IG1pc21hdGNoIGZvciAlcyIsIG5hbWUpCgkJfQoJfQoJdmVyaWZ5UmVndWxhcihnb0V4ZWN1dGFibGUpCgl2ZXJpZnlHbyhnb0V4ZWN1dGFibGUpCn0KCmZ1bmMgZW5hYmxlU3VicmVhcGVyKCkgewoJXywgXywgZXJybm8gOj0gc3lzY2FsbC5SYXdTeXNjYWxsNihzeXNjYWxsLlNZU19QUkNUTCwgcHJTZXRDaGlsZFN1YnJlYXBlciwgMSwgMCwgMCwgMCwgMCkKCWlmIGVycm5vICE9IDAgewoJCWZhaWwoImVuYWJsZSBjaGlsZCBzdWJyZWFwZXI6ICV2IiwgZXJybm8pCgl9Cn0KCmZ1bmMgZm9yd2FyZFNpZ25hbHMocHJvY2Vzc0dyb3VwIGludCwgc3RvcCA8LWNoYW4gc3RydWN0e30pIHsKCXNpZ25hbHMgOj0gbWFrZShjaGFuIG9zLlNpZ25hbCwgMTYpCglzaWduYWwuTm90aWZ5KAoJCXNpZ25hbHMsCgkJc3lzY2FsbC5TSUdIVVAsCgkJc3lzY2FsbC5TSUdJTlQsCgkJc3lzY2FsbC5TSUdRVUlULAoJCXN5c2NhbGwuU0lHVEVSTSwKCQlzeXNjYWxsLlNJR1VTUjEsCgkJc3lzY2FsbC5TSUdVU1IyLAoJCXN5c2NhbGwuU0lHVFNUUCwKCQlzeXNjYWxsLlNJR0NPTlQsCgkJc3lzY2FsbC5TSUdXSU5DSCwKCSkKCWRlZmVyIHNpZ25hbC5TdG9wKHNpZ25hbHMpCglmb3IgewoJCXNlbGVjdCB7CgkJY2FzZSByZWNlaXZlZCA6PSA8LXNpZ25hbHM6CgkJCWlmIHJlY2VpdmVkID09IG5pbCB7CgkJCQljb250aW51ZQoJCQl9CgkJCWlmIHR5cGVkLCBvayA6PSByZWNlaXZlZC4oc3lzY2FsbC5TaWduYWwpOyBvayB7CgkJCQlfID0gc3lzY2FsbC5LaWxsKC1wcm9jZXNzR3JvdXAsIHR5cGVkKQoJCQl9CgkJY2FzZSA8LXN0b3A6CgkJCXJldHVybgoJCX0KCX0KfQoKZnVuYyBleGl0Q29kZShzdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzKSBpbnQgewoJaWYgc3RhdHVzLkV4aXRlZCgpIHsKCQlyZXR1cm4gc3RhdHVzLkV4aXRTdGF0dXMoKQoJfQoJaWYgc3RhdHVzLlNpZ25hbGVkKCkgewoJCXJldHVybiAxMjggKyBpbnQoc3RhdHVzLlNpZ25hbCgpKQoJfQoJcmV0dXJuIDEKfQoKZnVuYyBydW5TZXJ2aWNlc0VudHJ5cG9pbnQocnVubmVyIHN0cmluZywgYXJndW1lbnRzIFtdc3RyaW5nKSB7CglpZiBydW5uZXIgIT0gc2VydmljZXNSdW5uZXJQYXRoKCkgewoJCWZhaWwoInByb3RlY3RlZCBlbnRyeXBvaW50IHJlcXVpcmVzIGV4YWN0IHJ1bm5lciAlcyIsIHNlcnZpY2VzUnVubmVyUGF0aCgpKQoJfQoJaWYgZmlsZXBhdGguQ2xlYW4ob3MuQXJnc1swXSkgIT0gc2VydmljZXNFbnRyeXBvaW50UGF0aCgpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZW50cnlwb2ludCBtdXN0IHJ1biBmcm9tICVzIiwgc2VydmljZXNFbnRyeXBvaW50UGF0aCgpKQoJfQoJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZShzZXJ2aWNlc0VudHJ5cG9pbnRQYXRoKCkpCgl2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHJ1bm5lcikKCWVuYWJsZVN1YnJlYXBlcigpCgoJY29tbWFuZCA6PSBleGVjLkNvbW1hbmQocnVubmVyLCBhcmd1bWVudHMuLi4pCgljb21tYW5kLkVudiA9IG9zLkVudmlyb24oKQoJY29tbWFuZC5TdGRpbiA9IG9zLlN0ZGluCgljb21tYW5kLlN0ZG91dCA9IG9zLlN0ZG91dAoJY29tbWFuZC5TdGRlcnIgPSBvcy5TdGRlcnIKCWNvbW1hbmQuU3lzUHJvY0F0dHIgPSAmc3lzY2FsbC5TeXNQcm9jQXR0cntTZXRwZ2lkOiB0cnVlfQoJaWYgZXJyIDo9IGNvbW1hbmQuU3RhcnQoKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgic3RhcnQgcHJvdGVjdGVkIHJ1bm5lcjogJXYiLCBlcnIpCgl9CgoJc3RvcEZvcndhcmRpbmcgOj0gbWFrZShjaGFuIHN0cnVjdHt9KQoJZ28gZm9yd2FyZFNpZ25hbHMoY29tbWFuZC5Qcm9jZXNzLlBpZCwgc3RvcEZvcndhcmRpbmcpCgl2YXIgcnVubmVyU3RhdHVzIHN5c2NhbGwuV2FpdFN0YXR1cwoJZm9yIHsKCQl2YXIgc3RhdHVzIHN5c2NhbGwuV2FpdFN0YXR1cwoJCXBpZCwgZXJyIDo9IHN5c2NhbGwuV2FpdDQoLTEsICZzdGF0dXMsIDAsIG5pbCkKCQlpZiBlcnIgPT0gc3lzY2FsbC5FSU5UUiB7CgkJCWNvbnRpbnVlCgkJfQoJCWlmIGVyciAhPSBuaWwgewoJCQljbG9zZShzdG9wRm9yd2FyZGluZykKCQkJZmFpbCgid2FpdCBmb3IgcHJvdGVjdGVkIHJ1bm5lcjogJXYiLCBlcnIpCgkJfQoJCWlmIHBpZCA9PSBjb21tYW5kLlByb2Nlc3MuUGlkIHsKCQkJcnVubmVyU3RhdHVzID0gc3RhdHVzCgkJCWJyZWFrCgkJfQoJfQoJY2xvc2Uoc3RvcEZvcndhcmRpbmcpCglvcy5FeGl0KGV4aXRDb2RlKHJ1bm5lclN0YXR1cykpCn0KCmZ1bmMgbWFpbigpIHsKCWlmIGxlbihvcy5BcmdzKSA9PSAzICYmIG9zLkFyZ3NbMV0gPT0gInByZXBhcmUiIHsKCQlwcmVwYXJlKG9zLkFyZ3NbMl0pCgkJcmV0dXJuCgl9CglpZiBsZW4ob3MuQXJncykgPT0gNCAmJiBvcy5BcmdzWzFdID09ICJpbnN0YWxsIiB7CgkJaW5zdGFsbChvcy5BcmdzWzJdLCBvcy5BcmdzWzNdKQoJCXJldHVybgoJfQoJaWYgbGVuKG9zLkFyZ3MpID09IDMgJiYgb3MuQXJnc1sxXSA9PSAidmVyaWZ5IiB7CgkJdmVyaWZ5R28ob3MuQXJnc1syXSkKCQlyZXR1cm4KCX0KCWlmIGxlbihvcy5BcmdzKSA+PSAzICYmIG9zLkFyZ3NbMV0gPT0gImVudHJ5cG9pbnQiIHsKCQlydW5TZXJ2aWNlc0VudHJ5cG9pbnQob3MuQXJnc1syXSwgb3MuQXJnc1szOl0pCgkJcmV0dXJuCgl9CglmYWlsKCJ1bnN1cHBvcnRlZCBpbnZvY2F0aW9uIikKfQo=' | base64 -d > /tmp/mattercodex-go-toolchain-guard.go \
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
COPY --from=0 /bin/busybox /opt/mattercodex/protected-artifacts/mattercodex-shell
ENV GOROOT=/usr/local/go \
  GOENV=off \
  GOFLAGS= \
  GOTOOLCHAIN=local \
  PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin
RUN ["/mattercodex-go-toolchain-guard", "install", "deploy", "/usr/local/go/bin/go"]

USER 10001:10001

CMD ["/usr/local/bin/mattercodex-shell", "sh"]
