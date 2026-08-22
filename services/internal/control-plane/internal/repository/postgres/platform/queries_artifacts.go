package platform

import _ "embed"

var (
	//go:embed sql/artifacts_uploadartifact_1.sql
	queryArtifactsUploadartifact1 string
	//go:embed sql/artifacts_uploadartifact_2.sql
	queryArtifactsUploadartifact2 string
	//go:embed sql/artifacts_uploadartifact_3.sql
	queryArtifactsUploadartifact3 string
	//go:embed sql/artifacts_uploadartifact_4.sql
	queryArtifactsUploadartifact4 string
	//go:embed sql/artifacts_uploadartifact_5.sql
	queryArtifactsUploadartifact5 string
	//go:embed sql/artifacts_uploadartifact_6.sql
	queryArtifactsUploadartifact6 string
	//go:embed sql/artifacts_downloadartifact_1.sql
	queryArtifactsDownloadartifact1 string
	//go:embed sql/artifacts_changeartifactbinding_1.sql
	queryArtifactsChangeartifactbinding1 string
	//go:embed sql/artifacts_changeartifactbinding_2.sql
	queryArtifactsChangeartifactbinding2 string
	//go:embed sql/artifacts_changeartifactbinding_3.sql
	queryArtifactsChangeartifactbinding3 string
	//go:embed sql/artifacts_changeartifactbinding_4.sql
	queryArtifactsChangeartifactbinding4 string
)
