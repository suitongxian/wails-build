package similarity

import (
	"strconv"

	"data-asset-scan-go/internal/repository"
)

// Keys used in system_config for similarity thresholds. Stored as plain
// strings so they show up uniformly in the existing config UI.
const (
	CfgKeySameContent     = "similarity.same_content_threshold"
	CfgKeyProcessVersion  = "similarity.process_version_threshold"
	CfgKeyDerived         = "similarity.derived_threshold"
	CfgKeyImage           = "similarity.image_threshold"
	CfgKeyFileNameSim     = "similarity.filename_similarity_threshold"
	CfgKeyFeatureSim      = "similarity.feature_similarity_threshold"
)

// LoadConfigFromDB returns a Config seeded with experiment defaults, then
// overridden by any values stored in system_config. Missing or invalid keys
// keep the default — no error.
func LoadConfigFromDB(repo *repository.SystemConfigRepository) *Config {
	cfg := defaultConfig()
	if repo == nil {
		return cfg
	}

	overrideF := func(key string, dst *float64) {
		v := repo.GetValue(key)
		if v == "" {
			return
		}
		f, err := strconv.ParseFloat(v, 64)
		if err == nil && f > 0 && f <= 1.0 {
			*dst = f
		}
	}
	overrideF(CfgKeySameContent, &cfg.SameContentThreshold)
	overrideF(CfgKeyProcessVersion, &cfg.ProcessVersionThreshold)
	overrideF(CfgKeyDerived, &cfg.DerivedFileThreshold)
	overrideF(CfgKeyImage, &cfg.ImageSimilarityThreshold)
	overrideF(CfgKeyFileNameSim, &cfg.FileNameSimilarityThresh)
	overrideF(CfgKeyFeatureSim, &cfg.FeatureSimilarityThresh)
	return cfg
}
