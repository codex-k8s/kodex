package managedkubernetes

// MaximumCredentialBytes bounds private identity material in storage and the strict parser.
// It is not a result or frame budget and never permits publishing kubeconfig in an RPC response.
// MaximumCredentialBytes ограничивает private identity material в storage и strict parser.
// Это не result/frame budget и не разрешение публикации kubeconfig в RPC response.
const MaximumCredentialBytes = 64 << 10
