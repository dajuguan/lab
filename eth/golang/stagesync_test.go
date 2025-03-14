package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type SyncStage string

type Sync struct {
	stages       []*Stage
	currentStage uint
}

type Stage struct {
	ID      SyncStage
	Forward func() error
	Unwind  func() error
	Prune   func() error
}

func (s *Sync) StageState(stage SyncStage) (*StageState, error) {
	// 模拟获取阶段状态
	return &StageState{Stage: stage, BlockNumber: 100}, nil
}

func (s *Sync) Run() error {
	for !s.IsDone() {
		stage := s.stages[s.currentStage]
		if stage.Forward != nil {
			if err := stage.Forward(); err != nil {
				return err
			}
		}
		s.NextStage()
	}
	return nil
}

func (s *Sync) RunUnwind() error {
	for s.currentStage > 0 {
		s.currentStage--
		stage := s.stages[s.currentStage]
		if stage.Unwind != nil {
			if err := stage.Unwind(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Sync) RunPrune() error {
	for _, stage := range s.stages {
		if stage.Prune != nil {
			if err := stage.Prune(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Sync) IsDone() bool {
	return s.currentStage >= uint(len(s.stages))
}

func (s *Sync) NextStage() {
	s.currentStage++
}

type StageState struct {
	Stage       SyncStage
	BlockNumber uint64
}

func StageLoop(ctx context.Context, sync *Sync, loopMinTime time.Duration) {
	count := 1
	for {
		// lots of sync stages
		if count >= 3 {
			break
		}
		select {
		case <-ctx.Done():
			return
		default:
			// continue
		}

		err := sync.Run()
		if err != nil {
			fmt.Println("Error during sync run:", err)
			time.Sleep(500 * time.Millisecond) // avoid too many similar error logs
			continue
		}
		count++
	}
}

func TestSync(t *testing.T) {
	stages := []*Stage{
		{
			ID: "Stage1",
			Forward: func() error {
				fmt.Println("Running Stage1 Forward")
				time.Sleep(1 * time.Second)
				return nil
			},
			Unwind: func() error {
				fmt.Println("Running Stage1 Unwind")
				time.Sleep(1 * time.Second)
				return nil
			},
			Prune: func() error {
				fmt.Println("Running Stage1 Prune")
				time.Sleep(1 * time.Second)
				return nil
			},
		},
		{
			ID: "Stage2",
			Forward: func() error {
				fmt.Println("Running Stage2 Forward")
				time.Sleep(1 * time.Second)
				return nil
			},
			Unwind: func() error {
				fmt.Println("Running Stage2 Unwind")
				time.Sleep(1 * time.Second)
				return nil
			},
			Prune: func() error {
				fmt.Println("Running Stage2 Prune")
				time.Sleep(1 * time.Second)
				return nil
			},
		},
	}

	sync := &Sync{stages: stages}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("Starting Stage Loop")
	StageLoop(ctx, sync, 1*time.Second)
}
