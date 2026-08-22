package platform

import _ "embed"

var (
	//go:embed sql/workers_reconcilewarmruntime_1.sql
	queryWorkersReconcilewarmruntime1 string
	//go:embed sql/workers_reconcilewarmruntime_2.sql
	queryWorkersReconcilewarmruntime2 string
	//go:embed sql/workers_reportwarmruntime_1.sql
	queryWorkersReportwarmruntime1 string
	//go:embed sql/workers_claimdueschedules_1.sql
	queryWorkersClaimdueschedules1 string
	//go:embed sql/workers_claimdueschedules_2.sql
	queryWorkersClaimdueschedules2 string
	//go:embed sql/workers_changeoccurrence_1.sql
	queryWorkersChangeoccurrence1 string
	//go:embed sql/workers_changeoccurrence_2.sql
	queryWorkersChangeoccurrence2 string
	//go:embed sql/workers_changeoccurrence_3.sql
	queryWorkersChangeoccurrence3 string
	//go:embed sql/workers_changeoccurrence_4.sql
	queryWorkersChangeoccurrence4 string
	//go:embed sql/workers_changeoccurrence_5.sql
	queryWorkersChangeoccurrence5 string
	//go:embed sql/workers_changeoccurrence_6.sql
	queryWorkersChangeoccurrence6 string
	//go:embed sql/workers_mustscheduleref_1.sql
	queryWorkersMustscheduleref1 string
	//go:embed sql/workers_resolveintegrationinvocation_1.sql
	queryWorkersResolveintegrationinvocation1 string
	//go:embed sql/workers_resolveintegrationinvocation_2.sql
	queryWorkersResolveintegrationinvocation2 string
	//go:embed sql/workers_completeintegrationinvocation_1.sql
	queryWorkersCompleteintegrationinvocation1 string
	//go:embed sql/workers_completeintegrationinvocation_2.sql
	queryWorkersCompleteintegrationinvocation2 string
)
