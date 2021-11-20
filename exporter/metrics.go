package exporter

type metrics struct {
	DB     *db
	Pools  []pool
	Memory memory
}

type db struct {
	Version uint32
	Time    string
}

type pool struct {
	State       string
	Primary     bool
	Threads     threads
	QueueLength int64
}

type threads struct {
	Live        *int64
	Idle        *int64
	Max         *int64
	IdleTimeout *int64
}

type memory struct {
	Heap       *uint64
	Mmap       *uint64
	Used       *uint64
	Free       *uint64
	Releasable *uint64
	PoolsUsed  *uint64
	PoolsTotal *uint64
}
