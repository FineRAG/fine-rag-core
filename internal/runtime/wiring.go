package runtime

import (
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/governance"
	"enterprise-go-rag/internal/services/ingestion"
)

type PersistenceWiring struct {
	TenantRegistry contracts.TenantRegistryRepository
	MetadataRepo   contracts.IngestionMetadataRepository
	AuditRepo      contracts.AuditEventRepository
}

func NewGovernanceService(wiring PersistenceWiring) *governance.DeterministicPolicyService {
	if wiring.AuditRepo == nil {
		return governance.NewDeterministicPolicyService(nil)
	}
	return governance.NewDeterministicPolicyServiceWithRepository(wiring.AuditRepo)
}

func NewMetadataRecorder(wiring PersistenceWiring) *ingestion.MetadataRecorder {
	return ingestion.NewMetadataRecorder(wiring.MetadataRepo)
}
