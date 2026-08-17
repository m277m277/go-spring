/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package main is the self-contained example for starter-xxljob. It starts a
// tiny mock xxl-job admin (just enough of the REST surface to register an
// executor and trigger a run), starts the executor, registers a handler,
// triggers the job through the mock admin, and asserts the handler ran. No
// real xxl-job-admin needed — the protocol under test is the executor's side.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"go-spring.org/log"
	"go-spring.org/spring/gs"

	starter "go-spring.org/starter-xxljob"
)

const adminAddr = "127.0.0.1:18081"

// handlerRan signals a successful task execution.
var handlerRan = make(chan struct{}, 1)

type Service struct {
	Executor *starter.Executor `autowire:"a"`
}

// Init registers the handler after the executor bean is injected.
func (s *Service) Init() error {
	s.Executor.RegisterHandler("demoJob", func(ctx context.Context, param string) error {
		if param != "a=1" {
			return fmt.Errorf("unexpected param %q", param)
		}
		select {
		case handlerRan <- struct{}{}:
		default:
		}
		return nil
	})
	return nil
}

var manual = flag.Bool("manual", false, "run in manual verification mode (server stays up)")

func main() {
	flag.Parse()

	// Mock admin: /api/registry (accept register/heartbeat), /api/registry/remove,
	// and /api/trigger which calls back into the executor's /run.
	go mockAdmin()

	svrBean := gs.Provide(&Service{}).Export(gs.As[gs.Rooter]()).Init((*Service).Init)

	if !*manual {
		go func() {
			time.Sleep(1 * time.Second)
			runTest(svrBean.Interface().(*Service))
		}()
	} else {
		fmt.Println("=== Manual verification mode ===")
		fmt.Println("Server is running. Follow the README commands in another terminal.")
		fmt.Println("Press Ctrl+C to stop.")
	}
	gs.Run()
}

// mockAdmin serves just enough xxl-job admin REST to exercise the executor:
// registry endpoints no-op, and /trigger POSTs a TriggerParam to the
// executor's /run and returns its response.
func mockAdmin() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/registry", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/registry/remove", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/trigger", func(w http.ResponseWriter, r *http.Request) {
		param := starter.TriggerParam{
			JobID: 1, ExecutorHandler: "demoJob", ExecutorParams: "a=1", LogID: 1,
			ExecutorTimeout: 30, LogDateTime: time.Now().UnixMilli(),
		}
		body, _ := json.Marshal(param)
		resp, err := http.Post("http://127.0.0.1:9999/run", "application/json", bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		_ = json.NewEncoder(w).Encode(map[string]int{"code": 200})
	})
	_ = http.ListenAndServe(adminAddr, mux)
}

func runTest(s *Service) {
	ctx := context.Background()

	resp, err := http.Post("http://"+adminAddr+"/api/trigger", "application/json", nil)
	if err != nil {
		log.Errorf(ctx, log.TagAppDef, "TRIGGER failed: %v", err)
		os.Exit(1)
	}
	_ = resp.Body.Close()

	select {
	case <-handlerRan:
		fmt.Println("xxl-job round trip OK: demoJob ran")
	case <-time.After(15 * time.Second):
		log.Errorf(ctx, log.TagAppDef, "HANDLER timed out")
		os.Exit(1)
	}

	syscall.Kill(os.Getpid(), syscall.SIGTERM)
}

func init() {
	var execDir string
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		execDir = filepath.Dir(filename)
	}
	if err := os.Chdir(execDir); err != nil {
		panic(err)
	}
	workDir, _ := os.Getwd()
	fmt.Println(workDir)
}
