package platform

import _ "embed"

// queryKubernetesSetupSourceShare prevents operator rotation during setup admission or grant issue.
// queryKubernetesSetupSourceShare предотвращает operator rotation во время setup admission либо grant issue.
//
//go:embed sql/kubernetes_setup__source_share.sql
var queryKubernetesSetupSourceShare string

// queryKubernetesSetupSerializeActor serializes bounded admission capacity across distinct Connections.
// It acquires no subject row lock that could reverse the ordinary Connection-to-subject lock order.
// queryKubernetesSetupSerializeActor сериализует bounded admission capacity между разными Connections.
// Не берёт subject row lock, который мог бы обратить обычный порядок Connection-to-subject locks.
//
//go:embed sql/kubernetes_setup__serialize_actor.sql
var queryKubernetesSetupSerializeActor string

// queryKubernetesSetupBeginContext reads public configuration and one database clock under the Connection lock.
// queryKubernetesSetupBeginContext читает public configuration и один database clock под Connection lock.
//
//go:embed sql/kubernetes_setup__begin_context.sql
var queryKubernetesSetupBeginContext string

// queryKubernetesSetupBeginReceipt resolves the permanent original actor key without another permit.
// queryKubernetesSetupBeginReceipt разрешает permanent original actor key без нового permit.
//
//go:embed sql/kubernetes_setup__begin_receipt.sql
var queryKubernetesSetupBeginReceipt string

// queryKubernetesSetupCapacity retains ambiguous material outcomes in the actor's bounded admission count.
// queryKubernetesSetupCapacity учитывает ambiguous material outcomes в bounded admission count actor.
//
//go:embed sql/kubernetes_setup__capacity.sql
var queryKubernetesSetupCapacity string

// queryKubernetesSetupInsert persists immutable metadata before any broker storage request is possible.
// queryKubernetesSetupInsert сохраняет immutable metadata до возможности broker storage request.
//
//go:embed sql/kubernetes_setup__insert.sql
var queryKubernetesSetupInsert string
