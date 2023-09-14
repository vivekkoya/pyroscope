package validation

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/grafana/pyroscope/pkg/phlaredb/block"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v3"
)

const (
	bytesInMB = 1048576

	// MinCompactorPartialBlockDeletionDelay is the minimum partial blocks deletion delay that can be configured in Mimir.
	// Partial blocks are blocks that are not having meta file uploaded yet.
	MinCompactorPartialBlockDeletionDelay = 4 * time.Hour
)

// Limits describe all the limits for tenants; can be used to describe global default
// limits via flags, or per-tenant limits via yaml config.
// NOTE: we use custom `model.Duration` instead of standard `time.Duration` because,
// to support tenant-friendly duration format (e.g: "1h30m45s") in JSON value.
type Limits struct {
	// Distributor enforced limits.
	IngestionRateMB        float64 `yaml:"ingestion_rate_mb" json:"ingestion_rate_mb"`
	IngestionBurstSizeMB   float64 `yaml:"ingestion_burst_size_mb" json:"ingestion_burst_size_mb"`
	MaxLabelNameLength     int     `yaml:"max_label_name_length" json:"max_label_name_length"`
	MaxLabelValueLength    int     `yaml:"max_label_value_length" json:"max_label_value_length"`
	MaxLabelNamesPerSeries int     `yaml:"max_label_names_per_series" json:"max_label_names_per_series"`

	MaxProfileSizeBytes              int `yaml:"max_profile_size_bytes" json:"max_profile_size_bytes"`
	MaxProfileStacktraceSamples      int `yaml:"max_profile_stacktrace_samples" json:"max_profile_stacktrace_samples"`
	MaxProfileStacktraceSampleLabels int `yaml:"max_profile_stacktrace_sample_labels" json:"max_profile_stacktrace_sample_labels"`
	MaxProfileStacktraceDepth        int `yaml:"max_profile_stacktrace_depth" json:"max_profile_stacktrace_depth"`
	MaxProfileSymbolValueLength      int `yaml:"max_profile_symbol_value_length" json:"max_profile_symbol_value_length"`

	// The tenant shard size determines the how many ingesters a particular
	// tenant will be sharded to. Needs to be specified on distributors for
	// correct distribution and on ingesters so that the local ingestion limit
	// can be calculated correctly.
	IngestionTenantShardSize int `yaml:"ingestion_tenant_shard_size" json:"ingestion_tenant_shard_size"`

	// Ingester enforced limits.
	MaxLocalSeriesPerTenant  int `yaml:"max_local_series_per_tenant" json:"max_local_series_per_tenant"`
	MaxGlobalSeriesPerTenant int `yaml:"max_global_series_per_tenant" json:"max_global_series_per_tenant"`

	// Querier enforced limits.
	MaxQueryLookback    model.Duration `yaml:"max_query_lookback" json:"max_query_lookback"`
	MaxQueryLength      model.Duration `yaml:"max_query_length" json:"max_query_length"`
	MaxQueryParallelism int            `yaml:"max_query_parallelism" json:"max_query_parallelism"`

	// Store-gateway.
	StoreGatewayTenantShardSize int `yaml:"store_gateway_tenant_shard_size" json:"store_gateway_tenant_shard_size"`

	// Query frontend.
	QuerySplitDuration model.Duration `yaml:"split_queries_by_interval" json:"split_queries_by_interval"`

	// Compactor.
	CompactorBlocksRetentionPeriod     model.Duration `yaml:"compactor_blocks_retention_period" json:"compactor_blocks_retention_period"`
	CompactorSplitAndMergeShards       int            `yaml:"compactor_split_and_merge_shards" json:"compactor_split_and_merge_shards"`
	CompactorSplitGroups               int            `yaml:"compactor_split_groups" json:"compactor_split_groups"`
	CompactorTenantShardSize           int            `yaml:"compactor_tenant_shard_size" json:"compactor_tenant_shard_size"`
	CompactorPartialBlockDeletionDelay model.Duration `yaml:"compactor_partial_block_deletion_delay" json:"compactor_partial_block_deletion_delay"`

	// This config doesn't have a CLI flag registered here because they're registered in
	// their own original config struct.
	S3SSEType                 string `yaml:"s3_sse_type" json:"s3_sse_type" doc:"nocli|description=S3 server-side encryption type. Required to enable server-side encryption overrides for a specific tenant. If not set, the default S3 client settings are used."`
	S3SSEKMSKeyID             string `yaml:"s3_sse_kms_key_id" json:"s3_sse_kms_key_id" doc:"nocli|description=S3 server-side encryption KMS Key ID. Ignored if the SSE type override is not set."`
	S3SSEKMSEncryptionContext string `yaml:"s3_sse_kms_encryption_context" json:"s3_sse_kms_encryption_context" doc:"nocli|description=S3 server-side encryption KMS encryption context. If unset and the key ID override is set, the encryption context will not be provided to S3. Ignored if the SSE type override is not set."`
}

// LimitError are errors that do not comply with the limits specified.
type LimitError string

func (e LimitError) Error() string {
	return string(e)
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (l *Limits) RegisterFlags(f *flag.FlagSet) {
	f.Float64Var(&l.IngestionRateMB, "distributor.ingestion-rate-limit-mb", 4, "Per-tenant ingestion rate limit in sample size per second. Units in MB.")
	f.Float64Var(&l.IngestionBurstSizeMB, "distributor.ingestion-burst-size-mb", 2, "Per-tenant allowed ingestion burst size (in sample size). Units in MB. The burst size refers to the per-distributor local rate limiter, and should be set at least to the maximum profile size expected in a single push request.")

	f.IntVar(&l.IngestionTenantShardSize, "distributor.ingestion-tenant-shard-size", 0, "The tenant's shard size used by shuffle-sharding. Must be set both on ingesters and distributors. 0 disables shuffle sharding.")

	f.IntVar(&l.MaxLabelNameLength, "validation.max-length-label-name", 1024, "Maximum length accepted for label names.")
	f.IntVar(&l.MaxLabelValueLength, "validation.max-length-label-value", 2048, "Maximum length accepted for label value. This setting also applies to the metric name.")
	f.IntVar(&l.MaxLabelNamesPerSeries, "validation.max-label-names-per-series", 30, "Maximum number of label names per series.")

	f.IntVar(&l.MaxLocalSeriesPerTenant, "ingester.max-local-series-per-tenant", 0, "Maximum number of active series of profiles per tenant, per ingester. 0 to disable.")
	f.IntVar(&l.MaxGlobalSeriesPerTenant, "ingester.max-global-series-per-tenant", 5000, "Maximum number of active series of profiles per tenant, across the cluster. 0 to disable. When the global limit is enabled, each ingester is configured with a dynamic local limit based on the replication factor and the current number of healthy ingesters, and is kept updated whenever the number of ingesters change.")

	_ = l.MaxQueryLength.Set("24h")
	f.Var(&l.MaxQueryLength, "querier.max-query-length", "The limit to length of queries. 0 to disable.")

	_ = l.MaxQueryLookback.Set("7d")
	f.Var(&l.MaxQueryLookback, "querier.max-query-lookback", "Limit how far back in profiling data can be queried, up until lookback duration ago. This limit is enforced in the query frontend. If the requested time range is outside the allowed range, the request will not fail, but will be modified to only query data within the allowed time range. 0 to disable, default to 7d.")

	f.IntVar(&l.StoreGatewayTenantShardSize, "store-gateway.tenant-shard-size", 0, "The tenant's shard size, used when store-gateway sharding is enabled. Value of 0 disables shuffle sharding for the tenant, that is all tenant blocks are sharded across all store-gateway replicas.")

	_ = l.QuerySplitDuration.Set("0s")
	f.Var(&l.QuerySplitDuration, "querier.split-queries-by-interval", "Split queries by a time interval and execute in parallel. The value 0 disables splitting by time")

	f.IntVar(&l.MaxQueryParallelism, "querier.max-query-parallelism", 0, "Maximum number of queries that will be scheduled in parallel by the frontend.")

	f.IntVar(&l.MaxProfileSizeBytes, "validation.max-profile-size-bytes", 4*1024*1024, "Maximum size of a profile in bytes. This is based off the uncompressed size. 0 to disable.")
	f.IntVar(&l.MaxProfileStacktraceSamples, "validation.max-profile-stacktrace-samples", 16000, "Maximum number of samples in a profile. 0 to disable.")
	f.IntVar(&l.MaxProfileStacktraceSampleLabels, "validation.max-profile-stacktrace-sample-labels", 100, "Maximum number of labels in a profile sample. 0 to disable.")
	f.IntVar(&l.MaxProfileStacktraceDepth, "validation.max-profile-stacktrace-depth", 1000, "Maximum depth of a profile stacktrace. Profiles are not rejected instead stacktraces are truncated. 0 to disable.")
	f.IntVar(&l.MaxProfileSymbolValueLength, "validation.max-profile-symbol-value-length", 65535, "Maximum length of a profile symbol value (labels, function names and filenames, etc...). Profiles are not rejected instead symbol values are truncated. 0 to disable.")

	f.Var(&l.CompactorBlocksRetentionPeriod, "compactor.blocks-retention-period", "Delete blocks containing samples older than the specified retention period. 0 to disable.")
	f.IntVar(&l.CompactorSplitAndMergeShards, "compactor.split-and-merge-shards", 0, "The number of shards to use when splitting blocks. 0 to disable splitting.")
	f.IntVar(&l.CompactorSplitGroups, "compactor.split-groups", 1, "Number of groups that blocks for splitting should be grouped into. Each group of blocks is then split separately. Number of output split shards is controlled by -compactor.split-and-merge-shards.")
	f.IntVar(&l.CompactorTenantShardSize, "compactor.compactor-tenant-shard-size", 0, "Max number of compactors that can compact blocks for single tenant. 0 to disable the limit and use all compactors.")
	_ = l.CompactorPartialBlockDeletionDelay.Set("1d")
	f.Var(&l.CompactorPartialBlockDeletionDelay, "compactor.partial-block-deletion-delay", fmt.Sprintf("If a partial block (unfinished block without %s file) hasn't been modified for this time, it will be marked for deletion. The minimum accepted value is %s: a lower value will be ignored and the feature disabled. 0 to disable.", block.MetaFilename, MinCompactorPartialBlockDeletionDelay.String()))
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (l *Limits) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// We want to set c to the defaults and then overwrite it with the input.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.  See prometheus/config.
	type plain Limits

	// During startup we wont have a default value so we don't want to overwrite them
	if defaultLimits != nil {
		b, err := yaml.Marshal(defaultLimits)
		if err != nil {
			return errors.Wrap(err, "cloning limits (marshaling)")
		}
		if err := yaml.Unmarshal(b, (*plain)(l)); err != nil {
			return errors.Wrap(err, "cloning limits (unmarshaling)")
		}
	}
	return unmarshal((*plain)(l))
}

// Validate validates that this limits config is valid.
func (l *Limits) Validate() error {
	return nil
}

// When we load YAML from disk, we want the various per-customer limits
// to default to any values specified on the command line, not default
// command line values.  This global contains those values.  I (Tom) cannot
// find a nicer way I'm afraid.
var defaultLimits *Limits

// SetDefaultLimitsForYAMLUnmarshalling sets global default limits, used when loading
// Limits from YAML files. This is used to ensure per-tenant limits are defaulted to
// those values.
func SetDefaultLimitsForYAMLUnmarshalling(defaults Limits) {
	defaultLimits = &defaults
}

type TenantLimits interface {
	// TenantLimits is a function that returns limits for given tenant, or
	// nil, if there are no tenant-specific limits.
	TenantLimits(tenantID string) *Limits
	// AllByTenantID gets a mapping of all tenant IDs and limits for that tenant
	AllByTenantID() map[string]*Limits
}

// Overrides periodically fetch a set of per-tenant overrides, and provides convenience
// functions for fetching the correct value.
type Overrides struct {
	defaultLimits *Limits
	tenantLimits  TenantLimits
}

// NewOverrides makes a new Overrides.
func NewOverrides(defaults Limits, tenantLimits TenantLimits) (*Overrides, error) {
	return &Overrides{
		tenantLimits:  tenantLimits,
		defaultLimits: &defaults,
	}, nil
}

func (o *Overrides) AllByTenantID() map[string]*Limits {
	if o.tenantLimits != nil {
		return o.tenantLimits.AllByTenantID()
	}
	return nil
}

// IngestionRateBytes returns the limit on ingester rate (MBs per second).
func (o *Overrides) IngestionRateBytes(tenantID string) float64 {
	return o.getOverridesForTenant(tenantID).IngestionRateMB * bytesInMB
}

// IngestionBurstSizeBytes returns the burst size for ingestion rate.
func (o *Overrides) IngestionBurstSizeBytes(tenantID string) int {
	return int(o.getOverridesForTenant(tenantID).IngestionBurstSizeMB * bytesInMB)
}

// IngestionTenantShardSize returns the ingesters shard size for a given user.
func (o *Overrides) IngestionTenantShardSize(tenantID string) int {
	return o.getOverridesForTenant(tenantID).IngestionTenantShardSize
}

// MaxLabelNameLength returns maximum length a label name can be.
func (o *Overrides) MaxLabelNameLength(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxLabelNameLength
}

// MaxLabelValueLength returns maximum length a label value can be. This also is
// the maximum length of a metric name.
func (o *Overrides) MaxLabelValueLength(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxLabelValueLength
}

// MaxLabelNamesPerSeries returns maximum number of label/value pairs timeseries.
func (o *Overrides) MaxLabelNamesPerSeries(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxLabelNamesPerSeries
}

// MaxProfileSizeBytes returns the maximum size of a profile in bytes.
func (o *Overrides) MaxProfileSizeBytes(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxProfileSizeBytes
}

// MaxProfileStacktraceSamples returns the maximum number of samples in a profile.
func (o *Overrides) MaxProfileStacktraceSamples(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxProfileStacktraceSamples
}

// MaxProfileStacktraceSampleLabels returns the maximum number of labels in a profile sample.
func (o *Overrides) MaxProfileStacktraceSampleLabels(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxProfileStacktraceSampleLabels
}

// MaxProfileStacktraceDepth returns the maximum depth of a profile stacktrace.
func (o *Overrides) MaxProfileStacktraceDepth(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxProfileStacktraceDepth
}

// MaxProfileSymbolValueLength returns the maximum length of a profile symbol value (labels, function name and filename, etc...).
func (o *Overrides) MaxProfileSymbolValueLength(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxProfileSymbolValueLength
}

// MaxLocalSeriesPerTenant returns the maximum number of series a tenant is allowed to store
// in a single ingester.
func (o *Overrides) MaxLocalSeriesPerTenant(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxLocalSeriesPerTenant
}

// MaxGlobalSeriesPerTenant returns the maximum number of series a tenant is allowed to store
// across the cluster.
func (o *Overrides) MaxGlobalSeriesPerTenant(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxGlobalSeriesPerTenant
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (o *Overrides) MaxQueryLength(tenantID string) time.Duration {
	return time.Duration(o.getOverridesForTenant(tenantID).MaxQueryLength)
}

// MaxQueryParallelism returns the limit to the number of sub-queries the
// frontend will process in parallel.
func (o *Overrides) MaxQueryParallelism(tenantID string) int {
	return o.getOverridesForTenant(tenantID).MaxQueryParallelism
}

// MaxQueryLookback returns the max lookback period of queries.
func (o *Overrides) MaxQueryLookback(tenantID string) time.Duration {
	return time.Duration(o.getOverridesForTenant(tenantID).MaxQueryLookback)
}

// StoreGatewayTenantShardSize returns the store-gateway shard size for a given user.
func (o *Overrides) StoreGatewayTenantShardSize(userID string) int {
	return o.getOverridesForTenant(userID).StoreGatewayTenantShardSize
}

// QuerySplitDuration returns the tenant specific split by interval applied in the query frontend.
func (o *Overrides) QuerySplitDuration(tenantID string) time.Duration {
	return time.Duration(o.getOverridesForTenant(tenantID).QuerySplitDuration)
}

// CompactorTenantShardSize returns number of compactors that this user can use. 0 = all compactors.
func (o *Overrides) CompactorTenantShardSize(userID string) int {
	return o.getOverridesForTenant(userID).CompactorTenantShardSize
}

// CompactorBlocksRetentionPeriod returns the retention period for a given user.
func (o *Overrides) CompactorBlocksRetentionPeriod(userID string) time.Duration {
	return time.Duration(o.getOverridesForTenant(userID).CompactorBlocksRetentionPeriod)
}

// CompactorSplitAndMergeShards returns the number of shards to use when splitting blocks.
func (o *Overrides) CompactorSplitAndMergeShards(userID string) int {
	return o.getOverridesForTenant(userID).CompactorSplitAndMergeShards
}

// CompactorSplitGroups returns the number of groups that blocks for splitting should be grouped into.
func (o *Overrides) CompactorSplitGroups(userID string) int {
	return o.getOverridesForTenant(userID).CompactorSplitGroups
}

// CompactorPartialBlockDeletionDelay returns the partial block deletion delay time period for a given user,
// and whether the configured value was valid. If the value wasn't valid, the returned delay is the default one
// and the caller is responsible to warn the Mimir operator about it.
func (o *Overrides) CompactorPartialBlockDeletionDelay(userID string) (delay time.Duration, valid bool) {
	delay = time.Duration(o.getOverridesForTenant(userID).CompactorPartialBlockDeletionDelay)

	// Forcefully disable partial blocks deletion if the configured delay is too low.
	if delay > 0 && delay < MinCompactorPartialBlockDeletionDelay {
		return 0, false
	}

	return delay, true
}

// S3SSEType returns the per-tenant S3 SSE type.
func (o *Overrides) S3SSEType(user string) string {
	return o.getOverridesForTenant(user).S3SSEType
}

// S3SSEKMSKeyID returns the per-tenant S3 KMS-SSE key id.
func (o *Overrides) S3SSEKMSKeyID(user string) string {
	return o.getOverridesForTenant(user).S3SSEKMSKeyID
}

// S3SSEKMSEncryptionContext returns the per-tenant S3 KMS-SSE encryption context.
func (o *Overrides) S3SSEKMSEncryptionContext(user string) string {
	return o.getOverridesForTenant(user).S3SSEKMSEncryptionContext
}

// MaxQueriersPerTenant returns the limit to the number of queriers that can be used
// Shuffle sharding will be used to distribute queries across queriers.
// 0 means no limit. Currently disabled.
func (o *Overrides) MaxQueriersPerTenant(tenant string) int { return 0 }

func (o *Overrides) DefaultLimits() *Limits {
	return o.defaultLimits
}

func (o *Overrides) getOverridesForTenant(tenantID string) *Limits {
	if o.tenantLimits != nil {
		l := o.tenantLimits.TenantLimits(tenantID)
		if l != nil {
			return l
		}
	}
	return o.defaultLimits
}

// OverwriteMarshalingStringMap will overwrite the src map when unmarshaling
// as opposed to merging.
type OverwriteMarshalingStringMap struct {
	m map[string]string
}

func NewOverwriteMarshalingStringMap(m map[string]string) OverwriteMarshalingStringMap {
	return OverwriteMarshalingStringMap{m: m}
}

func (sm *OverwriteMarshalingStringMap) Map() map[string]string {
	return sm.m
}

// MarshalJSON explicitly uses the the type receiver and not pointer receiver
// or it won't be called
func (sm OverwriteMarshalingStringMap) MarshalJSON() ([]byte, error) {
	return json.Marshal(sm.m)
}

func (sm *OverwriteMarshalingStringMap) UnmarshalJSON(val []byte) error {
	var def map[string]string
	if err := json.Unmarshal(val, &def); err != nil {
		return err
	}
	sm.m = def

	return nil
}

// MarshalYAML explicitly uses the the type receiver and not pointer receiver
// or it won't be called
func (sm OverwriteMarshalingStringMap) MarshalYAML() (interface{}, error) {
	return sm.m, nil
}

func (sm *OverwriteMarshalingStringMap) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var def map[string]string

	err := unmarshal(&def)
	if err != nil {
		return err
	}
	sm.m = def

	return nil
}
