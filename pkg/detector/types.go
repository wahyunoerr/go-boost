package detector

type ProjectStack struct {
	RootPath         string          `json:"root_path"`
	ModuleName       string          `json:"module_name"`
	GoVersion        string          `json:"go_version"`
	Framework        string          `json:"framework"`
	FrameworkVersion string          `json:"framework_version"`
	ORM              string          `json:"orm"`
	ORMVersion       string          `json:"orm_version"`
	DatabaseEngine   string          `json:"database_engine"`
	CacheEngine      string          `json:"cache_engine"`
	QueueEngine      string          `json:"queue_engine"`
	ConfigManager    string          `json:"config_manager"`
	RPC              string          `json:"rpc"`
	Logger           string          `json:"logger"`
	Validator        string          `json:"validator"`
	TestFramework    string          `json:"test_framework"`
	Architecture     string          `json:"architecture"`
	Packages         []PackageInfo   `json:"packages"`
	DetectedDirs     map[string]bool `json:"detected_dirs"`
	Layout           *ProjectLayout  `json:"layout,omitempty"`
}

type PackageInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Indirect bool   `json:"indirect"`
}
