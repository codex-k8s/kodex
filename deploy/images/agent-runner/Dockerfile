FROM golang:1.26.5-alpine AS scratch

RUN mkdir -p /out /tmp/mattercodex-go-build-cache \
  && printf '%s' 'cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImNyeXB0by9zaGEyNTYiCgkiZm10IgoJImlvIgoJIm9zIgoJIm9zL2V4ZWMiCgkicGF0aC9maWxlcGF0aCIKCSJzb3J0IgoJInN0cmluZ3MiCgkic3lzY2FsbCIKKQoKY29uc3QgKAoJdHJ1c3RlZEdPUk9PVCAgICAgID0gIi91c3IvbG9jYWwvZ28iCglib290c3RyYXBHT1JPT1QgICAgPSAiL29wdC9tYXR0ZXJjb2RleC9ib290c3RyYXAtZ28iCglwcm90ZWN0ZWRTdGFnaW5nICAgPSAiL29wdC9tYXR0ZXJjb2RleC9wcm90ZWN0ZWQtYXJ0aWZhY3RzIgoJcHJvdGVjdGVkVGFyZ2V0RGlyID0gIi91c3IvbG9jYWwvYmluIgoJcHJvdGVjdGVkUm9vdCAgICAgID0gIi8iCgl0cnVzdGVkVUlEICAgICAgICAgPSAwCgl0cnVzdGVkR0lEICAgICAgICAgPSAwCikKCnZhciBwcm90ZWN0ZWRUb29scyA9IFtdc3RyaW5newoJImJ1ZiIsCgkiZ29mdW1wdCIsCgkiZ29pbXBvcnRzIiwKCSJnb2xhbmdjaS1saW50IiwKCSJnb29zZSIsCgkiZ3JwY3VybCIsCgkibW9ja2dlbiIsCgkib2FwaS1jb2RlZ2VuIiwKCSJwcm90b2MtZ2VuLWdvIiwKCSJwcm90b2MtZ2VuLWdvLWdycGMiLAoJInNxbGMiLAoJInN0YXRpY2NoZWNrIiwKCSJ5cSIsCn0KCmZ1bmMgZmFpbChmb3JtYXQgc3RyaW5nLCB2YWx1ZXMgLi4uYW55KSB7CglmbXQuRnByaW50Zihvcy5TdGRlcnIsIGZvcm1hdCsiXG4iLCB2YWx1ZXMuLi4pCglvcy5FeGl0KDEpCn0KCmZ1bmMgdHJ1c3RlZEdvQ29tbWFuZChnb0V4ZWN1dGFibGUgc3RyaW5nLCBhcmd1bWVudHMgLi4uc3RyaW5nKSAqZXhlYy5DbWQgewoJY29tbWFuZCA6PSBleGVjLkNvbW1hbmQoZ29FeGVjdXRhYmxlLCBhcmd1bWVudHMuLi4pCgljb21tYW5kLkVudiA9IFtdc3RyaW5newoJCSJQQVRIPS91c3IvbG9jYWwvZ28vYmluOi91c3IvYmluOi9iaW4iLAoJCSJIT01FPS90bXAiLAoJCSJHT1JPT1Q9IiArIHRydXN0ZWRHT1JPT1QsCgkJIkdPRU5WPW9mZiIsCgkJIkdPRkxBR1M9IiwKCQkiR09UT09MQ0hBSU49bG9jYWwiLAoJCSJHT1dPUks9b2ZmIiwKCX0KCXJldHVybiBjb21tYW5kCn0KCmZ1bmMgZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgbGFiZWwsIGV4cGVjdGVkIHN0cmluZywgYXJndW1lbnRzIC4uLnN0cmluZykgewoJb3V0cHV0LCBlcnIgOj0gdHJ1c3RlZEdvQ29tbWFuZChnb0V4ZWN1dGFibGUsIGFyZ3VtZW50cy4uLikuT3V0cHV0KCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoIiVzIGZhaWxlZDogJXYiLCBsYWJlbCwgZXJyKQoJfQoJaWYgIWJ5dGVzLkVxdWFsKG91dHB1dCwgW11ieXRlKGV4cGVjdGVkKSkgewoJCWZhaWwoIiVzIG1pc21hdGNoOiBnb3QgJXEsIHdhbnQgJXEiLCBsYWJlbCwgb3V0cHV0LCBleHBlY3RlZCkKCX0KfQoKZnVuYyB2ZXJpZnlHbyhnb0V4ZWN1dGFibGUgc3RyaW5nKSB7CglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09WRVJTSU9OIiwgImdvMS4yNi41XG4iLCAiZW52IiwgIkdPVkVSU0lPTiIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09UT09MQ0hBSU4iLCAibG9jYWxcbiIsICJlbnYiLCAiR09UT09MQ0hBSU4iKQoJZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgIkdPUk9PVCIsIHRydXN0ZWRHT1JPT1QrIlxuIiwgImVudiIsICJHT1JPT1QiKQoJZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgImdvIHRvb2wgY29tcGlsZSIsICJjb21waWxlIHZlcnNpb24gZ28xLjI2LjVcbiIsICJ0b29sIiwgImNvbXBpbGUiLCAiLVY9ZnVsbCIpCn0KCmZ1bmMgcHJvdGVjdGVkTmFtZXMocHJvZmlsZSBzdHJpbmcpIFtdc3RyaW5nIHsKCW5hbWVzIDo9IGFwcGVuZChbXXN0cmluZyhuaWwpLCBwcm90ZWN0ZWRUb29scy4uLikKCXN3aXRjaCBwcm9maWxlIHsKCWNhc2UgInNlcnZpY2VzIjoKCQluYW1lcyA9IGFwcGVuZChuYW1lcywgIm1hdHRlci1jb2RleC1hZ2VudC1ydW5uZXIiKQoJY2FzZSAiZGVwbG95IjoKCWRlZmF1bHQ6CgkJZmFpbCgidW5zdXBwb3J0ZWQgcHJvZmlsZSAlcSIsIHByb2ZpbGUpCgl9Cglzb3J0LlN0cmluZ3MobmFtZXMpCglyZXR1cm4gbmFtZXMKfQoKZnVuYyByZXF1aXJlVHJ1c3RlZElkZW50aXR5KCkgewoJaWYgb3MuR2V0ZXVpZCgpICE9IHRydXN0ZWRVSUQgfHwgb3MuR2V0ZWdpZCgpICE9IHRydXN0ZWRHSUQgewoJCWZhaWwoInRydXN0ZWQgZ3VhcmQgcmVxdWlyZXMgdWlkOmdpZCAlZDolZCIsIHRydXN0ZWRVSUQsIHRydXN0ZWRHSUQpCgl9Cn0KCmZ1bmMgc3RhdChwYXRoIHN0cmluZykgKG9zLkZpbGVJbmZvLCAqc3lzY2FsbC5TdGF0X3QpIHsKCWluZm8sIGVyciA6PSBvcy5Mc3RhdChwYXRoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgibHN0YXQgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJc3RhdHVzLCBvayA6PSBpbmZvLlN5cygpLigqc3lzY2FsbC5TdGF0X3QpCglpZiAhb2sgewoJCWZhaWwoInN0YXQgJXM6IHVuc3VwcG9ydGVkIHBsYXRmb3JtIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyBlbnN1cmVEaXJlY3RvcnkocGF0aCBzdHJpbmcpIHsKCXBhdGggPSBmaWxlcGF0aC5DbGVhbihwYXRoKQoJcm9vdCA6PSBmaWxlcGF0aC5DbGVhbihwcm90ZWN0ZWRSb290KQoJaWYgcGF0aCA9PSByb290IHsKCQl2ZXJpZnlEaXJlY3RvcnkocGF0aCkKCQlyZXR1cm4KCX0KCWlmIHJvb3QgIT0gIi8iICYmICFzdHJpbmdzLkhhc1ByZWZpeChwYXRoLCByb290K3N0cmluZyhvcy5QYXRoU2VwYXJhdG9yKSkgewoJCWZhaWwoInByb3RlY3RlZCBwYXRoICVzIGVzY2FwZXMgcm9vdCAlcyIsIHBhdGgsIHJvb3QpCgl9CglwYXJlbnQgOj0gZmlsZXBhdGguRGlyKHBhdGgpCgllbnN1cmVEaXJlY3RvcnkocGFyZW50KQoJaW5mbywgZXJyIDo9IG9zLkxzdGF0KHBhdGgpCglpZiBlcnIgPT0gbmlsICYmICFpbmZvLklzRGlyKCkgewoJCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocGF0aCk7IGVyciAhPSBuaWwgewoJCQlmYWlsKCJyZW1vdmUgdW5zYWZlIHBhdGggJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCQlpbmZvID0gbmlsCgl9IGVsc2UgaWYgZXJyICE9IG5pbCAmJiAhb3MuSXNOb3RFeGlzdChlcnIpIHsKCQlmYWlsKCJsc3RhdCAlczogJXYiLCBwYXRoLCBlcnIpCgl9CglpZiBpbmZvID09IG5pbCB7CgkJaWYgZXJyIDo9IG9zLk1rZGlyKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCAmJiAhb3MuSXNFeGlzdChlcnIpIHsKCQkJZmFpbCgibWtkaXIgJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCX0KCWlmIGVyciA6PSBvcy5DaG93bihwYXRoLCB0cnVzdGVkVUlELCB0cnVzdGVkR0lEKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2hvd24gJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLkNobW9kKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2htb2QgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJdmVyaWZ5RGlyZWN0b3J5KHBhdGgpCn0KCmZ1bmMgdmVyaWZ5RGlyZWN0b3J5KHBhdGggc3RyaW5nKSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uSXNEaXIoKSB8fCBpbmZvLk1vZGUoKSZvcy5Nb2RlU3ltbGluayAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCBhIHJlYWwgZGlyZWN0b3J5IiwgcGF0aCkKCX0KCWlmIHN0YXR1cy5VaWQgIT0gdWludDMyKHRydXN0ZWRVSUQpIHx8IHN0YXR1cy5HaWQgIT0gdWludDMyKHRydXN0ZWRHSUQpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCByb290LW93bmVkIiwgcGF0aCkKCX0KCWlmIGluZm8uTW9kZSgpLlBlcm0oKSYwbzAyMiAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KfQoKZnVuYyB2ZXJpZnlQYXJlbnRzKHBhdGggc3RyaW5nKSB7Cglyb290IDo9IGZpbGVwYXRoLkNsZWFuKHByb3RlY3RlZFJvb3QpCglmb3IgY3VycmVudCA6PSBmaWxlcGF0aC5DbGVhbihwYXRoKTsgOyBjdXJyZW50ID0gZmlsZXBhdGguRGlyKGN1cnJlbnQpIHsKCQl2ZXJpZnlEaXJlY3RvcnkoY3VycmVudCkKCQlpZiBjdXJyZW50ID09IHJvb3QgewoJCQlyZXR1cm4KCQl9Cgl9Cn0KCmZ1bmMgcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUgc3RyaW5nKSB7Cglmb3IgXywgbmFtZSA6PSByYW5nZSBwcm90ZWN0ZWROYW1lcyhwcm9maWxlKSB7CgkJcGF0aCA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgbmFtZSkKCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHBhdGgpOyBlcnIgIT0gbmlsIHsKCQkJZmFpbCgicmVtb3ZlIHByb3RlY3RlZCBkZXN0aW5hdGlvbiAlczogJXYiLCBwYXRoLCBlcnIpCgkJfQoJfQp9CgpmdW5jIHByZXBhcmUocHJvZmlsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJZW5zdXJlRGlyZWN0b3J5KGZpbGVwYXRoLkRpcih0cnVzdGVkR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIoYm9vdHN0cmFwR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIocHJvdGVjdGVkU3RhZ2luZykpCgllbnN1cmVEaXJlY3RvcnkocHJvdGVjdGVkVGFyZ2V0RGlyKQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbCh0cnVzdGVkR09ST09UKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xlYW4gJXM6ICV2IiwgdHJ1c3RlZEdPUk9PVCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbChib290c3RyYXBHT1JPT1QpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbGVhbiAlczogJXYiLCBib290c3RyYXBHT1JPT1QsIGVycikKCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocHJvdGVjdGVkU3RhZ2luZyk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNsZWFuICVzOiAldiIsIHByb3RlY3RlZFN0YWdpbmcsIGVycikKCX0KCWVuc3VyZURpcmVjdG9yeSh0cnVzdGVkR09ST09UKQoJZW5zdXJlRGlyZWN0b3J5KHByb3RlY3RlZFN0YWdpbmcpCglyZW1vdmVQcm90ZWN0ZWRMZWF2ZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHModHJ1c3RlZEdPUk9PVCkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQp9CgpmdW5jIHZlcmlmeVJlZ3VsYXIocGF0aCBzdHJpbmcpIChvcy5GaWxlSW5mbywgKnN5c2NhbGwuU3RhdF90KSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uTW9kZSgpLklzUmVndWxhcigpIHx8IGluZm8uTW9kZSgpJm9zLk1vZGVTeW1saW5rICE9IDAgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3QgYSByZWd1bGFyIG5vbi1zeW1saW5rIGZpbGUiLCBwYXRoKQoJfQoJaWYgc3RhdHVzLlVpZCAhPSB1aW50MzIodHJ1c3RlZFVJRCkgfHwgc3RhdHVzLkdpZCAhPSB1aW50MzIodHJ1c3RlZEdJRCkgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3Qgcm9vdC1vd25lZCIsIHBhdGgpCgl9CglpZiBzdGF0dXMuTmxpbmsgIT0gMSB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGhhcyAlZCBoYXJkbGlua3MiLCBwYXRoLCBzdGF0dXMuTmxpbmspCgl9CglpZiBpbmZvLk1vZGUoKS5QZXJtKCkmMG8wMjIgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyBkaWdlc3QocGF0aCBzdHJpbmcpIFtzaGEyNTYuU2l6ZV1ieXRlIHsKCWZpbGUsIGVyciA6PSBvcy5PcGVuKHBhdGgpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJvcGVuICVzOiAldiIsIHBhdGgsIGVycikKCX0KCWRlZmVyIGZpbGUuQ2xvc2UoKQoJaGFzaCA6PSBzaGEyNTYuTmV3KCkKCWlmIF8sIGVyciA6PSBpby5Db3B5KGhhc2gsIGZpbGUpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJoYXNoICVzOiAldiIsIHBhdGgsIGVycikKCX0KCXZhciByZXN1bHQgW3NoYTI1Ni5TaXplXWJ5dGUKCWNvcHkocmVzdWx0WzpdLCBoYXNoLlN1bShuaWwpKQoJcmV0dXJuIHJlc3VsdAp9CgpmdW5jIGNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQgc3RyaW5nKSB7Cglzb3VyY2VGaWxlLCBlcnIgOj0gb3MuT3Blbihzb3VyY2UpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJvcGVuIHByb3RlY3RlZCBzb3VyY2UgJXM6ICV2Iiwgc291cmNlLCBlcnIpCgl9CglkZWZlciBzb3VyY2VGaWxlLkNsb3NlKCkKCXRhcmdldEZpbGUsIGVyciA6PSBvcy5PcGVuRmlsZSh0YXJnZXQsIG9zLk9fV1JPTkxZfG9zLk9fQ1JFQVRFfG9zLk9fRVhDTCwgMG81NTUpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjcmVhdGUgcHJvdGVjdGVkIGRlc3RpbmF0aW9uICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgXywgZXJyIDo9IGlvLkNvcHkodGFyZ2V0RmlsZSwgc291cmNlRmlsZSk7IGVyciAhPSBuaWwgewoJCXRhcmdldEZpbGUuQ2xvc2UoKQoJCWZhaWwoImNvcHkgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuU3luYygpOyBlcnIgIT0gbmlsIHsKCQl0YXJnZXRGaWxlLkNsb3NlKCkKCQlmYWlsKCJzeW5jIHByb3RlY3RlZCBhcnRpZmFjdCAlczogJXYiLCB0YXJnZXQsIGVycikKCX0KCWlmIGVyciA6PSB0YXJnZXRGaWxlLkNobW9kKDBvNTU1KTsgZXJyICE9IG5pbCB7CgkJdGFyZ2V0RmlsZS5DbG9zZSgpCgkJZmFpbCgiY2htb2QgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuQ2xvc2UoKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xvc2UgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQp9CgpmdW5jIHZlcmlmeUV4YWN0U3RhZ2luZyhuYW1lcyBbXXN0cmluZykgewoJZW50cmllcywgZXJyIDo9IG9zLlJlYWREaXIocHJvdGVjdGVkU3RhZ2luZykKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoInJlYWQgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJaWYgbGVuKGVudHJpZXMpICE9IGxlbihuYW1lcykgewoJCWZhaWwoInByb3RlY3RlZCBzdGFnaW5nIGhhcyAlZCBhcnRpZmFjdHMsIHdhbnQgJWQiLCBsZW4oZW50cmllcyksIGxlbihuYW1lcykpCgl9Cglmb3IgaW5kZXgsIGVudHJ5IDo9IHJhbmdlIGVudHJpZXMgewoJCWlmIGVudHJ5Lk5hbWUoKSAhPSBuYW1lc1tpbmRleF0gewoJCQlmYWlsKCJwcm90ZWN0ZWQgc3RhZ2luZyBhcnRpZmFjdCAlcSwgd2FudCAlcSIsIGVudHJ5Lk5hbWUoKSwgbmFtZXNbaW5kZXhdKQoJCX0KCX0KfQoKZnVuYyBpbnN0YWxsKHByb2ZpbGUsIGdvRXhlY3V0YWJsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJbmFtZXMgOj0gcHJvdGVjdGVkTmFtZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQoJdmVyaWZ5UGFyZW50cyh0cnVzdGVkR09ST09UKQoJdmVyaWZ5RXhhY3RTdGFnaW5nKG5hbWVzKQoJc291cmNlRGlnZXN0cyA6PSBtYWtlKG1hcFtzdHJpbmddW3NoYTI1Ni5TaXplXWJ5dGUsIGxlbihuYW1lcykpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJc291cmNlIDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkU3RhZ2luZywgbmFtZSkKCQl2ZXJpZnlSZWd1bGFyKHNvdXJjZSkKCQlzb3VyY2VEaWdlc3RzW25hbWVdID0gZGlnZXN0KHNvdXJjZSkKCX0KCXJlbW92ZVByb3RlY3RlZExlYXZlcyhwcm9maWxlKQoJZm9yIF8sIG5hbWUgOj0gcmFuZ2UgbmFtZXMgewoJCXNvdXJjZSA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFN0YWdpbmcsIG5hbWUpCgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCWNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQpCgkJdmVyaWZ5UmVndWxhcih0YXJnZXQpCgkJaWYgZGlnZXN0KHRhcmdldCkgIT0gc291cmNlRGlnZXN0c1tuYW1lXSB7CgkJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCBkaWdlc3QgbWlzbWF0Y2ggZm9yICVzIiwgbmFtZSkKCQl9Cgl9CglpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHByb3RlY3RlZFN0YWdpbmcpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJyZW1vdmUgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRUYXJnZXREaXIpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCXZlcmlmeVJlZ3VsYXIodGFyZ2V0KQoJCWlmIGRpZ2VzdCh0YXJnZXQpICE9IHNvdXJjZURpZ2VzdHNbbmFtZV0gewoJCQlmYWlsKCJmaW5hbCBwcm90ZWN0ZWQgYXJ0aWZhY3QgZGlnZXN0IG1pc21hdGNoIGZvciAlcyIsIG5hbWUpCgkJfQoJfQoJdmVyaWZ5UmVndWxhcihnb0V4ZWN1dGFibGUpCgl2ZXJpZnlHbyhnb0V4ZWN1dGFibGUpCn0KCmZ1bmMgbWFpbigpIHsKCWlmIGxlbihvcy5BcmdzKSA9PSAzICYmIG9zLkFyZ3NbMV0gPT0gInByZXBhcmUiIHsKCQlwcmVwYXJlKG9zLkFyZ3NbMl0pCgkJcmV0dXJuCgl9CglpZiBsZW4ob3MuQXJncykgPT0gNCAmJiBvcy5BcmdzWzFdID09ICJpbnN0YWxsIiB7CgkJaW5zdGFsbChvcy5BcmdzWzJdLCBvcy5BcmdzWzNdKQoJCXJldHVybgoJfQoJaWYgbGVuKG9zLkFyZ3MpID09IDMgJiYgb3MuQXJnc1sxXSA9PSAidmVyaWZ5IiB7CgkJdmVyaWZ5R28ob3MuQXJnc1syXSkKCQlyZXR1cm4KCX0KCWZhaWwoInVuc3VwcG9ydGVkIGludm9jYXRpb24iKQp9Cg==' | base64 -d > /tmp/mattercodex-go-toolchain-guard.go \
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

COPY --from=0 /out/mattercodex-go-toolchain-guard /mattercodex-go-toolchain-guard
RUN ["/mattercodex-go-toolchain-guard", "prepare", "deploy"]
COPY --from=0 /usr/local/go/ /usr/local/go/
COPY --from=1 /out/mattercodex-protected/ /opt/mattercodex/protected-artifacts/
ENV GOROOT=/usr/local/go \
  GOENV=off \
  GOFLAGS= \
  GOTOOLCHAIN=local \
  PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin
RUN ["/mattercodex-go-toolchain-guard", "install", "deploy", "/usr/local/go/bin/go"]

USER 10001:10001

CMD ["sh"]
