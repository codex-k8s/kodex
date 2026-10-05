FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build

ENV GOTOOLCHAIN=local

RUN test "$(go env GOVERSION)" = "go1.26.6"

RUN mkdir -p /out /tmp/kodex-go-build-cache \
	&& printf '%s' 'cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImNyeXB0by9zaGEyNTYiCgkiZm10IgoJImlvIgoJIm9zIgoJIm9zL2V4ZWMiCgkib3Mvc2lnbmFsIgoJInBhdGgvZmlsZXBhdGgiCgkic29ydCIKCSJzdHJpbmdzIgoJInN5c2NhbGwiCikKCmNvbnN0ICgKCXRydXN0ZWRHT1JPT1QgICAgICAgPSAiL3Vzci9sb2NhbC9nbyIKCWJvb3RzdHJhcEdPUk9PVCAgICAgPSAiL29wdC9rb2RleC9ib290c3RyYXAtZ28iCglwcm90ZWN0ZWRTdGFnaW5nICAgID0gIi9vcHQva29kZXgvcHJvdGVjdGVkLWFydGlmYWN0cyIKCXByb3RlY3RlZFRhcmdldERpciAgPSAiL3Vzci9sb2NhbC9iaW4iCglwcm90ZWN0ZWRSb290ICAgICAgID0gIi8iCgl0cnVzdGVkVUlEICAgICAgICAgID0gMAoJdHJ1c3RlZEdJRCAgICAgICAgICA9IDAKCXByU2V0Q2hpbGRTdWJyZWFwZXIgPSAzNgoJbWF4TGludXhTaWduYWwgICAgICA9IDY0CglnbGliY1NpZ25hbENhbmNlbCAgID0gMzIKCWdsaWJjU2lnbmFsU2V0WElEICAgPSAzMwopCgp2YXIgcHJvdGVjdGVkVG9vbHMgPSBbXXN0cmluZ3sKCSJidWYiLAoJImdvZnVtcHQiLAoJImdvaW1wb3J0cyIsCgkiZ29sYW5nY2ktbGludCIsCgkiZ29vc2UiLAoJImdycGN1cmwiLAoJIm1vY2tnZW4iLAoJIm9hcGktY29kZWdlbiIsCgkicHJvdG9jLWdlbi1nbyIsCgkicHJvdG9jLWdlbi1nby1ncnBjIiwKCSJzcWxjIiwKCSJzdGF0aWNjaGVjayIsCgkieXEiLAp9CgpmdW5jIGZhaWwoZm9ybWF0IHN0cmluZywgdmFsdWVzIC4uLmFueSkgewoJZm10LkZwcmludGYob3MuU3RkZXJyLCBmb3JtYXQrIlxuIiwgdmFsdWVzLi4uKQoJb3MuRXhpdCgxKQp9CgpmdW5jIGJvb3RzdHJhcEd1YXJkUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRSb290LCAia29kZXgtZ28tdG9vbGNoYWluLWd1YXJkIikKfQoKZnVuYyBzZXJ2aWNlc0VudHJ5cG9pbnRQYXRoKCkgc3RyaW5nIHsKCXJldHVybiBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgImtvZGV4LWluaXQiKQp9CgpmdW5jIHNlcnZpY2VzUnVubmVyUGF0aCgpIHN0cmluZyB7CglyZXR1cm4gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsICJrb2RleC1hZ2VudC1ydW5uZXIiKQp9CgpmdW5jIHRydXN0ZWRHb0NvbW1hbmQoZ29FeGVjdXRhYmxlIHN0cmluZywgYXJndW1lbnRzIC4uLnN0cmluZykgKmV4ZWMuQ21kIHsKCWNvbW1hbmQgOj0gZXhlYy5Db21tYW5kKGdvRXhlY3V0YWJsZSwgYXJndW1lbnRzLi4uKQoJY29tbWFuZC5FbnYgPSBbXXN0cmluZ3sKCQkiUEFUSD0vdXNyL2xvY2FsL2dvL2JpbjovdXNyL2JpbjovYmluIiwKCQkiSE9NRT0vdG1wIiwKCQkiR09ST09UPSIgKyB0cnVzdGVkR09ST09ULAoJCSJHT0VOVj1vZmYiLAoJCSJHT0ZMQUdTPSIsCgkJIkdPVE9PTENIQUlOPWxvY2FsIiwKCQkiR09XT1JLPW9mZiIsCgl9CglyZXR1cm4gY29tbWFuZAp9CgpmdW5jIGV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsIGxhYmVsLCBleHBlY3RlZCBzdHJpbmcsIGFyZ3VtZW50cyAuLi5zdHJpbmcpIHsKCW91dHB1dCwgZXJyIDo9IHRydXN0ZWRHb0NvbW1hbmQoZ29FeGVjdXRhYmxlLCBhcmd1bWVudHMuLi4pLk91dHB1dCgpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCIlcyBmYWlsZWQ6ICV2IiwgbGFiZWwsIGVycikKCX0KCWlmICFieXRlcy5FcXVhbChvdXRwdXQsIFtdYnl0ZShleHBlY3RlZCkpIHsKCQlmYWlsKCIlcyBtaXNtYXRjaDogZ290ICVxLCB3YW50ICVxIiwgbGFiZWwsIG91dHB1dCwgZXhwZWN0ZWQpCgl9Cn0KCmZ1bmMgdmVyaWZ5R28oZ29FeGVjdXRhYmxlIHN0cmluZykgewoJZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgIkdPVkVSU0lPTiIsICJnbzEuMjYuNlxuIiwgImVudiIsICJHT1ZFUlNJT04iKQoJZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgIkdPVE9PTENIQUlOIiwgImxvY2FsXG4iLCAiZW52IiwgIkdPVE9PTENIQUlOIikKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJHT1JPT1QiLCB0cnVzdGVkR09ST09UKyJcbiIsICJlbnYiLCAiR09ST09UIikKCWV4cGVjdE91dHB1dChnb0V4ZWN1dGFibGUsICJnbyB0b29sIGNvbXBpbGUiLCAiY29tcGlsZSB2ZXJzaW9uIGdvMS4yNi42XG4iLCAidG9vbCIsICJjb21waWxlIiwgIi1WPWZ1bGwiKQp9CgpmdW5jIHByb3RlY3RlZE5hbWVzKHByb2ZpbGUgc3RyaW5nKSBbXXN0cmluZyB7CgluYW1lcyA6PSBhcHBlbmQoW11zdHJpbmcobmlsKSwgcHJvdGVjdGVkVG9vbHMuLi4pCglzd2l0Y2ggcHJvZmlsZSB7CgljYXNlICJzZXJ2aWNlcyI6CgkJbmFtZXMgPSBhcHBlbmQobmFtZXMsICJrb2RleC1hZ2VudC1ydW5uZXIiLCAia29kZXgtaW5pdCIpCgljYXNlICJkZXBsb3kiOgoJCW5hbWVzID0gYXBwZW5kKG5hbWVzLCAiYnVzeWJveCIpCglkZWZhdWx0OgoJCWZhaWwoInVuc3VwcG9ydGVkIHByb2ZpbGUgJXEiLCBwcm9maWxlKQoJfQoJc29ydC5TdHJpbmdzKG5hbWVzKQoJcmV0dXJuIG5hbWVzCn0KCmZ1bmMgcmVxdWlyZVRydXN0ZWRJZGVudGl0eSgpIHsKCWlmIG9zLkdldGV1aWQoKSAhPSB0cnVzdGVkVUlEIHx8IG9zLkdldGVnaWQoKSAhPSB0cnVzdGVkR0lEIHsKCQlmYWlsKCJ0cnVzdGVkIGd1YXJkIHJlcXVpcmVzIHVpZDpnaWQgJWQ6JWQiLCB0cnVzdGVkVUlELCB0cnVzdGVkR0lEKQoJfQp9CgpmdW5jIHN0YXQocGF0aCBzdHJpbmcpIChvcy5GaWxlSW5mbywgKnN5c2NhbGwuU3RhdF90KSB7CglpbmZvLCBlcnIgOj0gb3MuTHN0YXQocGF0aCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoImxzdGF0ICVzOiAldiIsIHBhdGgsIGVycikKCX0KCXN0YXR1cywgb2sgOj0gaW5mby5TeXMoKS4oKnN5c2NhbGwuU3RhdF90KQoJaWYgIW9rIHsKCQlmYWlsKCJzdGF0ICVzOiB1bnN1cHBvcnRlZCBwbGF0Zm9ybSIsIHBhdGgpCgl9CglyZXR1cm4gaW5mbywgc3RhdHVzCn0KCmZ1bmMgZW5zdXJlRGlyZWN0b3J5KHBhdGggc3RyaW5nKSB7CglwYXRoID0gZmlsZXBhdGguQ2xlYW4ocGF0aCkKCXJvb3QgOj0gZmlsZXBhdGguQ2xlYW4ocHJvdGVjdGVkUm9vdCkKCWlmIHBhdGggPT0gcm9vdCB7CgkJdmVyaWZ5RGlyZWN0b3J5KHBhdGgpCgkJcmV0dXJuCgl9CglpZiByb290ICE9ICIvIiAmJiAhc3RyaW5ncy5IYXNQcmVmaXgocGF0aCwgcm9vdCtzdHJpbmcob3MuUGF0aFNlcGFyYXRvcikpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgcGF0aCAlcyBlc2NhcGVzIHJvb3QgJXMiLCBwYXRoLCByb290KQoJfQoJcGFyZW50IDo9IGZpbGVwYXRoLkRpcihwYXRoKQoJZW5zdXJlRGlyZWN0b3J5KHBhcmVudCkKCWluZm8sIGVyciA6PSBvcy5Mc3RhdChwYXRoKQoJaWYgZXJyID09IG5pbCAmJiAhaW5mby5Jc0RpcigpIHsKCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHBhdGgpOyBlcnIgIT0gbmlsIHsKCQkJZmFpbCgicmVtb3ZlIHVuc2FmZSBwYXRoICVzOiAldiIsIHBhdGgsIGVycikKCQl9CgkJaW5mbyA9IG5pbAoJfSBlbHNlIGlmIGVyciAhPSBuaWwgJiYgIW9zLklzTm90RXhpc3QoZXJyKSB7CgkJZmFpbCgibHN0YXQgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJaWYgaW5mbyA9PSBuaWwgewoJCWlmIGVyciA6PSBvcy5Na2RpcihwYXRoLCAwbzc1NSk7IGVyciAhPSBuaWwgJiYgIW9zLklzRXhpc3QoZXJyKSB7CgkJCWZhaWwoIm1rZGlyICVzOiAldiIsIHBhdGgsIGVycikKCQl9Cgl9CglpZiBlcnIgOj0gb3MuQ2hvd24ocGF0aCwgdHJ1c3RlZFVJRCwgdHJ1c3RlZEdJRCk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNob3duICVzOiAldiIsIHBhdGgsIGVycikKCX0KCWlmIGVyciA6PSBvcy5DaG1vZChwYXRoLCAwbzc1NSk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNobW9kICVzOiAldiIsIHBhdGgsIGVycikKCX0KCXZlcmlmeURpcmVjdG9yeShwYXRoKQp9CgpmdW5jIHZlcmlmeURpcmVjdG9yeShwYXRoIHN0cmluZykgewoJaW5mbywgc3RhdHVzIDo9IHN0YXQocGF0aCkKCWlmICFpbmZvLklzRGlyKCkgfHwgaW5mby5Nb2RlKCkmb3MuTW9kZVN5bWxpbmsgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGRpcmVjdG9yeSAlcyBpcyBub3QgYSByZWFsIGRpcmVjdG9yeSIsIHBhdGgpCgl9CglpZiBzdGF0dXMuVWlkICE9IHVpbnQzMih0cnVzdGVkVUlEKSB8fCBzdGF0dXMuR2lkICE9IHVpbnQzMih0cnVzdGVkR0lEKSB7CgkJZmFpbCgicHJvdGVjdGVkIGRpcmVjdG9yeSAlcyBpcyBub3Qgcm9vdC1vd25lZCIsIHBhdGgpCgl9CglpZiBpbmZvLk1vZGUoKS5QZXJtKCkmMG8wMjIgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGRpcmVjdG9yeSAlcyBpcyBncm91cC93b3JsZC13cml0YWJsZSIsIHBhdGgpCgl9Cn0KCmZ1bmMgdmVyaWZ5UGFyZW50cyhwYXRoIHN0cmluZykgewoJcm9vdCA6PSBmaWxlcGF0aC5DbGVhbihwcm90ZWN0ZWRSb290KQoJZm9yIGN1cnJlbnQgOj0gZmlsZXBhdGguQ2xlYW4ocGF0aCk7IDsgY3VycmVudCA9IGZpbGVwYXRoLkRpcihjdXJyZW50KSB7CgkJdmVyaWZ5RGlyZWN0b3J5KGN1cnJlbnQpCgkJaWYgY3VycmVudCA9PSByb290IHsKCQkJcmV0dXJuCgkJfQoJfQp9CgpmdW5jIHJlbW92ZVByb3RlY3RlZExlYXZlcyhwcm9maWxlIHN0cmluZykgewoJZm9yIF8sIG5hbWUgOj0gcmFuZ2UgcHJvdGVjdGVkTmFtZXMocHJvZmlsZSkgewoJCXBhdGggOj0gZmlsZXBhdGguSm9pbihwcm90ZWN0ZWRUYXJnZXREaXIsIG5hbWUpCgkJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbChwYXRoKTsgZXJyICE9IG5pbCB7CgkJCWZhaWwoInJlbW92ZSBwcm90ZWN0ZWQgZGVzdGluYXRpb24gJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCX0KfQoKZnVuYyBwcmVwYXJlKHByb2ZpbGUgc3RyaW5nKSB7CglyZXF1aXJlVHJ1c3RlZElkZW50aXR5KCkKCXZlcmlmeUJvb3RzdHJhcEd1YXJkKCkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIodHJ1c3RlZEdPUk9PVCkpCgllbnN1cmVEaXJlY3RvcnkoZmlsZXBhdGguRGlyKGJvb3RzdHJhcEdPUk9PVCkpCgllbnN1cmVEaXJlY3RvcnkoZmlsZXBhdGguRGlyKHByb3RlY3RlZFN0YWdpbmcpKQoJZW5zdXJlRGlyZWN0b3J5KHByb3RlY3RlZFRhcmdldERpcikKCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwodHJ1c3RlZEdPUk9PVCk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNsZWFuICVzOiAldiIsIHRydXN0ZWRHT1JPT1QsIGVycikKCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwoYm9vdHN0cmFwR09ST09UKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xlYW4gJXM6ICV2IiwgYm9vdHN0cmFwR09ST09ULCBlcnIpCgl9CglpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHByb3RlY3RlZFN0YWdpbmcpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbGVhbiAlczogJXYiLCBwcm90ZWN0ZWRTdGFnaW5nLCBlcnIpCgl9CgllbnN1cmVEaXJlY3RvcnkodHJ1c3RlZEdPUk9PVCkKCWVuc3VyZURpcmVjdG9yeShwcm90ZWN0ZWRTdGFnaW5nKQoJcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUpCgl2ZXJpZnlQYXJlbnRzKHRydXN0ZWRHT1JPT1QpCgl2ZXJpZnlQYXJlbnRzKHByb3RlY3RlZFN0YWdpbmcpCgl2ZXJpZnlQYXJlbnRzKHByb3RlY3RlZFRhcmdldERpcikKfQoKZnVuYyB2ZXJpZnlSZWd1bGFyKHBhdGggc3RyaW5nKSAob3MuRmlsZUluZm8sICpzeXNjYWxsLlN0YXRfdCkgewoJaW5mbywgc3RhdHVzIDo9IHN0YXQocGF0aCkKCWlmICFpbmZvLk1vZGUoKS5Jc1JlZ3VsYXIoKSB8fCBpbmZvLk1vZGUoKSZvcy5Nb2RlU3ltbGluayAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgYXJ0aWZhY3QgJXMgaXMgbm90IGEgcmVndWxhciBub24tc3ltbGluayBmaWxlIiwgcGF0aCkKCX0KCWlmIHN0YXR1cy5VaWQgIT0gdWludDMyKHRydXN0ZWRVSUQpIHx8IHN0YXR1cy5HaWQgIT0gdWludDMyKHRydXN0ZWRHSUQpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgYXJ0aWZhY3QgJXMgaXMgbm90IHJvb3Qtb3duZWQiLCBwYXRoKQoJfQoJaWYgc3RhdHVzLk5saW5rICE9IDEgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBoYXMgJWQgaGFyZGxpbmtzIiwgcGF0aCwgc3RhdHVzLk5saW5rKQoJfQoJaWYgaW5mby5Nb2RlKCkuUGVybSgpJjBvMDIyICE9IDAgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBncm91cC93b3JsZC13cml0YWJsZSIsIHBhdGgpCgl9CglyZXR1cm4gaW5mbywgc3RhdHVzCn0KCmZ1bmMgdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZShwYXRoIHN0cmluZykgewoJaW5mbywgXyA6PSB2ZXJpZnlSZWd1bGFyKHBhdGgpCglpZiBpbmZvLk1vZGUoKS5QZXJtKCkgIT0gMG81NTUgewoJCWZhaWwoInByb3RlY3RlZCBleGVjdXRhYmxlICVzIGhhcyBtb2RlICUwNG8sIHdhbnQgMDU1NSIsIHBhdGgsIGluZm8uTW9kZSgpLlBlcm0oKSkKCX0KCXZlcmlmeVBhcmVudHMoZmlsZXBhdGguRGlyKHBhdGgpKQp9CgpmdW5jIGRpZ2VzdChwYXRoIHN0cmluZykgW3NoYTI1Ni5TaXplXWJ5dGUgewoJZmlsZSwgZXJyIDo9IG9zLk9wZW4ocGF0aCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoIm9wZW4gJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJZGVmZXIgZmlsZS5DbG9zZSgpCgloYXNoIDo9IHNoYTI1Ni5OZXcoKQoJaWYgXywgZXJyIDo9IGlvLkNvcHkoaGFzaCwgZmlsZSk7IGVyciAhPSBuaWwgewoJCWZhaWwoImhhc2ggJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJdmFyIHJlc3VsdCBbc2hhMjU2LlNpemVdYnl0ZQoJY29weShyZXN1bHRbOl0sIGhhc2guU3VtKG5pbCkpCglyZXR1cm4gcmVzdWx0Cn0KCmZ1bmMgdmVyaWZ5Qm9vdHN0cmFwR3VhcmQoKSB7CglwYXRoIDo9IGJvb3RzdHJhcEd1YXJkUGF0aCgpCgl2ZXJpZnlJbnN0YWxsZWRFeGVjdXRhYmxlKHBhdGgpCglleGVjdXRhYmxlLCBlcnIgOj0gb3MuRXhlY3V0YWJsZSgpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJyZXNvbHZlIHJ1bm5pbmcgZ3VhcmQ6ICV2IiwgZXJyKQoJfQoJaWYgZGlnZXN0KHBhdGgpICE9IGRpZ2VzdChleGVjdXRhYmxlKSB7CgkJZmFpbCgicnVubmluZyBndWFyZCBkaWdlc3QgZGlmZmVycyBmcm9tIHByb3RlY3RlZCBib290c3RyYXAgZ3VhcmQiKQoJfQp9CgpmdW5jIGNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQgc3RyaW5nKSB7Cglzb3VyY2VGaWxlLCBlcnIgOj0gb3MuT3Blbihzb3VyY2UpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJvcGVuIHByb3RlY3RlZCBzb3VyY2UgJXM6ICV2Iiwgc291cmNlLCBlcnIpCgl9CglkZWZlciBzb3VyY2VGaWxlLkNsb3NlKCkKCXRhcmdldEZpbGUsIGVyciA6PSBvcy5PcGVuRmlsZSh0YXJnZXQsIG9zLk9fV1JPTkxZfG9zLk9fQ1JFQVRFfG9zLk9fRVhDTCwgMG81NTUpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjcmVhdGUgcHJvdGVjdGVkIGRlc3RpbmF0aW9uICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgXywgZXJyIDo9IGlvLkNvcHkodGFyZ2V0RmlsZSwgc291cmNlRmlsZSk7IGVyciAhPSBuaWwgewoJCXRhcmdldEZpbGUuQ2xvc2UoKQoJCWZhaWwoImNvcHkgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuU3luYygpOyBlcnIgIT0gbmlsIHsKCQl0YXJnZXRGaWxlLkNsb3NlKCkKCQlmYWlsKCJzeW5jIHByb3RlY3RlZCBhcnRpZmFjdCAlczogJXYiLCB0YXJnZXQsIGVycikKCX0KCWlmIGVyciA6PSB0YXJnZXRGaWxlLkNobW9kKDBvNTU1KTsgZXJyICE9IG5pbCB7CgkJdGFyZ2V0RmlsZS5DbG9zZSgpCgkJZmFpbCgiY2htb2QgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuQ2xvc2UoKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xvc2UgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQp9CgpmdW5jIHZlcmlmeUV4YWN0U3RhZ2luZyhuYW1lcyBbXXN0cmluZykgewoJZW50cmllcywgZXJyIDo9IG9zLlJlYWREaXIocHJvdGVjdGVkU3RhZ2luZykKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoInJlYWQgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJaWYgbGVuKGVudHJpZXMpICE9IGxlbihuYW1lcykgewoJCWZhaWwoInByb3RlY3RlZCBzdGFnaW5nIGhhcyAlZCBhcnRpZmFjdHMsIHdhbnQgJWQiLCBsZW4oZW50cmllcyksIGxlbihuYW1lcykpCgl9Cglmb3IgaW5kZXgsIGVudHJ5IDo9IHJhbmdlIGVudHJpZXMgewoJCWlmIGVudHJ5Lk5hbWUoKSAhPSBuYW1lc1tpbmRleF0gewoJCQlmYWlsKCJwcm90ZWN0ZWQgc3RhZ2luZyBhcnRpZmFjdCAlcSwgd2FudCAlcSIsIGVudHJ5Lk5hbWUoKSwgbmFtZXNbaW5kZXhdKQoJCX0KCX0KfQoKZnVuYyBpbnN0YWxsKHByb2ZpbGUsIGdvRXhlY3V0YWJsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJdmVyaWZ5Qm9vdHN0cmFwR3VhcmQoKQoJbmFtZXMgOj0gcHJvdGVjdGVkTmFtZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQoJdmVyaWZ5UGFyZW50cyh0cnVzdGVkR09ST09UKQoJdmVyaWZ5RXhhY3RTdGFnaW5nKG5hbWVzKQoJc291cmNlRGlnZXN0cyA6PSBtYWtlKG1hcFtzdHJpbmddW3NoYTI1Ni5TaXplXWJ5dGUsIGxlbihuYW1lcykpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJc291cmNlIDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkU3RhZ2luZywgbmFtZSkKCQl2ZXJpZnlSZWd1bGFyKHNvdXJjZSkKCQlzb3VyY2VEaWdlc3RzW25hbWVdID0gZGlnZXN0KHNvdXJjZSkKCX0KCXJlbW92ZVByb3RlY3RlZExlYXZlcyhwcm9maWxlKQoJZm9yIF8sIG5hbWUgOj0gcmFuZ2UgbmFtZXMgewoJCXNvdXJjZSA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFN0YWdpbmcsIG5hbWUpCgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCWNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQpCgkJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZSh0YXJnZXQpCgkJaWYgZGlnZXN0KHRhcmdldCkgIT0gc291cmNlRGlnZXN0c1tuYW1lXSB7CgkJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCBkaWdlc3QgbWlzbWF0Y2ggZm9yICVzIiwgbmFtZSkKCQl9Cgl9CglpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHByb3RlY3RlZFN0YWdpbmcpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJyZW1vdmUgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRUYXJnZXREaXIpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCXZlcmlmeUluc3RhbGxlZEV4ZWN1dGFibGUodGFyZ2V0KQoJCWlmIGRpZ2VzdCh0YXJnZXQpICE9IHNvdXJjZURpZ2VzdHNbbmFtZV0gewoJCQlmYWlsKCJmaW5hbCBwcm90ZWN0ZWQgYXJ0aWZhY3QgZGlnZXN0IG1pc21hdGNoIGZvciAlcyIsIG5hbWUpCgkJfQoJfQoJdmVyaWZ5UmVndWxhcihnb0V4ZWN1dGFibGUpCgl2ZXJpZnlHbyhnb0V4ZWN1dGFibGUpCn0KCmZ1bmMgZW5hYmxlU3VicmVhcGVyKCkgewoJXywgXywgZXJybm8gOj0gc3lzY2FsbC5SYXdTeXNjYWxsNihzeXNjYWxsLlNZU19QUkNUTCwgcHJTZXRDaGlsZFN1YnJlYXBlciwgMSwgMCwgMCwgMCwgMCkKCWlmIGVycm5vICE9IDAgewoJCWZhaWwoImVuYWJsZSBjaGlsZCBzdWJyZWFwZXI6ICV2IiwgZXJybm8pCgl9Cn0KCmZ1bmMgcHJveHlTaWduYWxzKCkgW11vcy5TaWduYWwgewoJc2lnbmFscyA6PSBtYWtlKFtdb3MuU2lnbmFsLCAwLCBtYXhMaW51eFNpZ25hbC03KQoJZm9yIG51bWJlciA6PSAxOyBudW1iZXIgPD0gbWF4TGludXhTaWduYWw7IG51bWJlcisrIHsKCQljdXJyZW50IDo9IHN5c2NhbGwuU2lnbmFsKG51bWJlcikKCQlzd2l0Y2ggY3VycmVudCB7CgkJY2FzZSBzeXNjYWxsLlNJR0tJTEwsCgkJCXN5c2NhbGwuU0lHU1RPUCwKCQkJc3lzY2FsbC5TSUdDSExELAoJCQlzeXNjYWxsLlNJR1VSRywKCQkJc3lzY2FsbC5TSUdQUk9GLAoJCQlzeXNjYWxsLlNpZ25hbChnbGliY1NpZ25hbENhbmNlbCksCgkJCXN5c2NhbGwuU2lnbmFsKGdsaWJjU2lnbmFsU2V0WElEKToKCQkJY29udGludWUKCQlkZWZhdWx0OgoJCQlzaWduYWxzID0gYXBwZW5kKHNpZ25hbHMsIGN1cnJlbnQpCgkJfQoJfQoJcmV0dXJuIHNpZ25hbHMKfQoKZnVuYyBmb3J3YXJkU2lnbmFscyhzaWduYWxzIDwtY2hhbiBvcy5TaWduYWwsIHByb2Nlc3NHcm91cCBpbnQsIHN0b3AgPC1jaGFuIHN0cnVjdHt9LCBkb25lIGNoYW48LSBzdHJ1Y3R7fSkgewoJZGVmZXIgY2xvc2UoZG9uZSkKCWZvciB7CgkJc2VsZWN0IHsKCQljYXNlIDwtc3RvcDoKCQkJcmV0dXJuCgkJZGVmYXVsdDoKCQl9CgkJc2VsZWN0IHsKCQljYXNlIHJlY2VpdmVkIDo9IDwtc2lnbmFsczoKCQkJaWYgcmVjZWl2ZWQgPT0gbmlsIHsKCQkJCWNvbnRpbnVlCgkJCX0KCQkJaWYgdHlwZWQsIG9rIDo9IHJlY2VpdmVkLihzeXNjYWxsLlNpZ25hbCk7IG9rIHsKCQkJCXNlbGVjdCB7CgkJCQljYXNlIDwtc3RvcDoKCQkJCQlyZXR1cm4KCQkJCWRlZmF1bHQ6CgkJCQl9CgkJCQlfID0gc3lzY2FsbC5LaWxsKC1wcm9jZXNzR3JvdXAsIHR5cGVkKQoJCQl9CgkJY2FzZSA8LXN0b3A6CgkJCXJldHVybgoJCX0KCX0KfQoKZnVuYyByZWFwRXhpdGVkQ2hpbGRyZW4oKSB7Cglmb3IgewoJCXZhciBzdGF0dXMgc3lzY2FsbC5XYWl0U3RhdHVzCgkJcGlkLCBlcnIgOj0gc3lzY2FsbC5XYWl0NCgtMSwgJnN0YXR1cywgc3lzY2FsbC5XTk9IQU5HLCBuaWwpCgkJc3dpdGNoIHsKCQljYXNlIGVyciA9PSBzeXNjYWxsLkVJTlRSOgoJCQljb250aW51ZQoJCWNhc2UgZXJyID09IHN5c2NhbGwuRUNISUxEOgoJCQlyZXR1cm4KCQljYXNlIGVyciAhPSBuaWw6CgkJCWZhaWwoInJlYXAgZXhpdGVkIGNoaWxkOiAldiIsIGVycikKCQljYXNlIHBpZCA9PSAwOgoJCQlyZXR1cm4KCQl9Cgl9Cn0KCmZ1bmMgZXhpdENvZGUoc3RhdHVzIHN5c2NhbGwuV2FpdFN0YXR1cykgaW50IHsKCWlmIHN0YXR1cy5FeGl0ZWQoKSB7CgkJcmV0dXJuIHN0YXR1cy5FeGl0U3RhdHVzKCkKCX0KCWlmIHN0YXR1cy5TaWduYWxlZCgpIHsKCQlyZXR1cm4gMTI4ICsgaW50KHN0YXR1cy5TaWduYWwoKSkKCX0KCXJldHVybiAxCn0KCmZ1bmMgcnVuU2VydmljZXNFbnRyeXBvaW50KHJ1bm5lciBzdHJpbmcsIGFyZ3VtZW50cyBbXXN0cmluZykgewoJaWYgcnVubmVyICE9IHNlcnZpY2VzUnVubmVyUGF0aCgpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZW50cnlwb2ludCByZXF1aXJlcyBleGFjdCBydW5uZXIgJXMiLCBzZXJ2aWNlc1J1bm5lclBhdGgoKSkKCX0KCWlmIGZpbGVwYXRoLkNsZWFuKG9zLkFyZ3NbMF0pICE9IHNlcnZpY2VzRW50cnlwb2ludFBhdGgoKSB7CgkJZmFpbCgicHJvdGVjdGVkIGVudHJ5cG9pbnQgbXVzdCBydW4gZnJvbSAlcyIsIHNlcnZpY2VzRW50cnlwb2ludFBhdGgoKSkKCX0KCXZlcmlmeUluc3RhbGxlZEV4ZWN1dGFibGUoc2VydmljZXNFbnRyeXBvaW50UGF0aCgpKQoJdmVyaWZ5SW5zdGFsbGVkRXhlY3V0YWJsZShydW5uZXIpCgllbmFibGVTdWJyZWFwZXIoKQoKCWNvbW1hbmQgOj0gZXhlYy5Db21tYW5kKHJ1bm5lciwgYXJndW1lbnRzLi4uKQoJY29tbWFuZC5FbnYgPSBvcy5FbnZpcm9uKCkKCWNvbW1hbmQuU3RkaW4gPSBvcy5TdGRpbgoJY29tbWFuZC5TdGRvdXQgPSBvcy5TdGRvdXQKCWNvbW1hbmQuU3RkZXJyID0gb3MuU3RkZXJyCgljb21tYW5kLlN5c1Byb2NBdHRyID0gJnN5c2NhbGwuU3lzUHJvY0F0dHJ7U2V0cGdpZDogdHJ1ZX0KCglwcm94aWVkU2lnbmFscyA6PSBwcm94eVNpZ25hbHMoKQoJc2lnbmFscyA6PSBtYWtlKGNoYW4gb3MuU2lnbmFsLCBsZW4ocHJveGllZFNpZ25hbHMpKjIpCglzaWduYWwuTm90aWZ5KHNpZ25hbHMsIHByb3hpZWRTaWduYWxzLi4uKQoJaWYgZXJyIDo9IGNvbW1hbmQuU3RhcnQoKTsgZXJyICE9IG5pbCB7CgkJc2lnbmFsLlN0b3Aoc2lnbmFscykKCQlmYWlsKCJzdGFydCBwcm90ZWN0ZWQgcnVubmVyOiAldiIsIGVycikKCX0KCglzdG9wRm9yd2FyZGluZyA6PSBtYWtlKGNoYW4gc3RydWN0e30pCglmb3J3YXJkaW5nRG9uZSA6PSBtYWtlKGNoYW4gc3RydWN0e30pCglnbyBmb3J3YXJkU2lnbmFscyhzaWduYWxzLCBjb21tYW5kLlByb2Nlc3MuUGlkLCBzdG9wRm9yd2FyZGluZywgZm9yd2FyZGluZ0RvbmUpCgl2YXIgcnVubmVyU3RhdHVzIHN5c2NhbGwuV2FpdFN0YXR1cwoJZm9yIHsKCQl2YXIgc3RhdHVzIHN5c2NhbGwuV2FpdFN0YXR1cwoJCXBpZCwgZXJyIDo9IHN5c2NhbGwuV2FpdDQoLTEsICZzdGF0dXMsIDAsIG5pbCkKCQlpZiBlcnIgPT0gc3lzY2FsbC5FSU5UUiB7CgkJCWNvbnRpbnVlCgkJfQoJCWlmIGVyciAhPSBuaWwgewoJCQljbG9zZShzdG9wRm9yd2FyZGluZykKCQkJc2lnbmFsLlN0b3Aoc2lnbmFscykKCQkJPC1mb3J3YXJkaW5nRG9uZQoJCQlmYWlsKCJ3YWl0IGZvciBwcm90ZWN0ZWQgcnVubmVyOiAldiIsIGVycikKCQl9CgkJaWYgcGlkID09IGNvbW1hbmQuUHJvY2Vzcy5QaWQgewoJCQlydW5uZXJTdGF0dXMgPSBzdGF0dXMKCQkJYnJlYWsKCQl9Cgl9CgljbG9zZShzdG9wRm9yd2FyZGluZykKCXNpZ25hbC5TdG9wKHNpZ25hbHMpCgk8LWZvcndhcmRpbmdEb25lCglyZWFwRXhpdGVkQ2hpbGRyZW4oKQoJb3MuRXhpdChleGl0Q29kZShydW5uZXJTdGF0dXMpKQp9CgpmdW5jIG1haW4oKSB7CglpZiBsZW4ob3MuQXJncykgPT0gMyAmJiBvcy5BcmdzWzFdID09ICJwcmVwYXJlIiB7CgkJcHJlcGFyZShvcy5BcmdzWzJdKQoJCXJldHVybgoJfQoJaWYgbGVuKG9zLkFyZ3MpID09IDQgJiYgb3MuQXJnc1sxXSA9PSAiaW5zdGFsbCIgewoJCWluc3RhbGwob3MuQXJnc1syXSwgb3MuQXJnc1szXSkKCQlyZXR1cm4KCX0KCWlmIGxlbihvcy5BcmdzKSA9PSAzICYmIG9zLkFyZ3NbMV0gPT0gInZlcmlmeSIgewoJCXZlcmlmeUdvKG9zLkFyZ3NbMl0pCgkJcmV0dXJuCgl9CglpZiBsZW4ob3MuQXJncykgPj0gMyAmJiBvcy5BcmdzWzFdID09ICJlbnRyeXBvaW50IiB7CgkJcnVuU2VydmljZXNFbnRyeXBvaW50KG9zLkFyZ3NbMl0sIG9zLkFyZ3NbMzpdKQoJCXJldHVybgoJfQoJZmFpbCgidW5zdXBwb3J0ZWQgaW52b2NhdGlvbiIpCn0K' | base64 -d > /tmp/kodex-go-toolchain-guard.go \
	&& /usr/bin/env -i PATH=/usr/local/go/bin:/usr/bin:/bin HOME=/tmp GOCACHE=/tmp/kodex-go-build-cache GOENV=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -buildvcs=false -o /out/kodex-go-toolchain-guard /tmp/kodex-go-toolchain-guard.go \
	&& rm -rf /tmp/kodex-go-toolchain-guard.go /tmp/kodex-go-build-cache

FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS runner-build

ENV GOTOOLCHAIN=local

WORKDIR /src/services/jobs/agent-runner

COPY services/jobs/agent-runner /src/services/jobs/agent-runner
COPY libs/go /src/libs/go/

RUN go mod download

RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN mkdir -p /out/kodex-protected \
	&& CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-X main.buildVersion=0.1.0' -o /out/kodex-protected/kodex-agent-runner ./cmd/agent-runner

# Локальный runtime сохраняет тот же защищенный entrypoint и UID-контракт, но
# не собирает полный набор инструментов роли. Production-образ остается
# последним stage этого Dockerfile и не зависит от local-runtime.
FROM docker.io/library/node:24-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e AS local-codex-cli

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
	--mount=type=cache,id=kodex-public-go-build-1.26.6,target=/root/.cache/go-build,sharing=shared \
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
	--mount=type=cache,id=kodex-public-go-build-1.26.6,target=/root/.cache/go-build,sharing=shared \
	set -eu; \
	apk add --no-cache ca-certificates git; \
	test "$(go env GOVERSION)" = "go1.26.6"; \
	mkdir -p "${GOBIN}"; \
	sh /usr/local/bin/kodex-build-go-tool github.com/cli/cli/v2/cmd/gh@"v${GH_CLI_VERSION}"; \
	sh /usr/local/bin/kodex-build-go-tool k8s.io/kubernetes/cmd/kubectl@"${KUBECTL_VERSION}"; \
	sh /usr/local/bin/kodex-build-go-tool helm.sh/helm/v4/cmd/helm@"${HELM_VERSION}"; \
	cli_version=$("${GOBIN}/gh" --version); \
	printf '%s\n' "$cli_version" | awk -v version="${GH_CLI_VERSION}" \
		'NR == 1 {valid=($1 == "gh" && $2 == "version" && $3 == version)} END {exit !valid}'; \
	client_version=$("${GOBIN}/kubectl" version --client -o json); \
	test "$(printf '%s\n' "$client_version" | sed -n 's/.*"gitVersion": "\([^"]*\)".*/\1/p')" = "${KUBECTL_VERSION}"; \
	test "$("${GOBIN}/helm" version --template '{{.Version}} {{.GoVersion}}')" = "${HELM_VERSION} go1.26.6"

FROM docker.io/library/node:24-bookworm@sha256:934240a162082fd8b8a2f90cd5114446443f1eba1c5378f6687167ca405e6584 AS full-runtime

ENV GOTOOLCHAIN=local

ARG JUST_VERSION=1.55.1
# Новая security-ревизия обновляет apt-слой, не затрагивая независимые Go tools.
ARG KODEX_TOOLCHAIN_SECURITY_REVISION=20261005
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

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"]
