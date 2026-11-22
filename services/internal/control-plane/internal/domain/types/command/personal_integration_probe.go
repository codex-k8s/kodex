package command

// PersonalIntegrationCredentialProbeContractVersion identifies the only supported finite probe admission protocol; zero and unknown versions never consume a lease.
// PersonalIntegrationCredentialProbeContractVersion задаёт единственную версию finite probe admission; нулевая и неизвестная версии не расходуют lease.
const PersonalIntegrationCredentialProbeContractVersion uint32 = 1

// PersonalIntegrationCredentialProbeAdmission carries only the owner-issued lease and explicit caller capability; it is separate from immutable completion receipt bytes.
// PersonalIntegrationCredentialProbeAdmission содержит лишь owner-issued lease и явную capability caller; immutable bytes completion receipt остаются отдельными.
type PersonalIntegrationCredentialProbeAdmission struct {
	TestRef, LeaseRef, Fence string
	Generation               int64
	ContractVersion          uint32
}
