// Copyright 2022 The Sigstore Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Copied from : https://github.com/google/trillian/blob/master/cmd/updatetree/main.go

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/google/trillian"
	"github.com/google/trillian/client/rpcflags"
	"google.golang.org/genproto/protobuf/field_mask"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var (
	adminServerAddr = flag.String("admin_server", "trillian-logserver.trillian-system.svc:8090", "Address of the gRPC Trillian Admin Server (host:port)")
	rpcDeadline     = flag.Duration("rpc_deadline", time.Second*10, "Deadline for RPC requests")
	treeState       = flag.String("tree_state", trillian.TreeState_FROZEN.String(), "State to update the tree with [default: FROZEN]")
	treeID          = flag.Int64("tree_id", 0, "The ID of the tree to update")
	treeType        = flag.String("tree_type", "", "If set the tree type will be updated")
	printTree       = flag.Bool("print", true, "Print the resulting tree")
)

func updateTree(ctx context.Context) (*trillian.Tree, error) {
	if *adminServerAddr == "" {
		return nil, errors.New("empty --admin_server, please provide the Admin server host:port")
	}

	tree := &trillian.Tree{TreeId: *treeID}
	paths := make([]string, 0)

	if len(*treeState) > 0 {
		m, err := protoregistry.GlobalTypes.FindEnumByName("trillian.TreeState")
		if err != nil {
			return nil, fmt.Errorf("can't find enum value map for states: %w", err)
		}
		newState := m.Descriptor().Values().ByName(protoreflect.Name(*treeState))
		if newState == nil {
			return nil, fmt.Errorf("invalid tree state: %v", *treeState)
		}
		tree.TreeState = trillian.TreeState(newState.Number())
		paths = append(paths, "tree_state")
	}

	if len(*treeType) > 0 {
		m, err := protoregistry.GlobalTypes.FindEnumByName("trillian.TreeType")
		if err != nil {
			return nil, fmt.Errorf("can't find enum value map for types: %w", err)
		}
		newType := m.Descriptor().Values().ByName(protoreflect.Name(*treeType))
		if newType == nil {
			return nil, fmt.Errorf("invalid tree type: %v", *treeType)
		}
		tree.TreeType = trillian.TreeType(newType.Number())
		paths = append(paths, "tree_type")
	}

	if len(paths) == 0 {
		return nil, errors.New("nothing to change")
	}

	// We only want to update certain fields of the tree, which means we
	// need a field mask on the request.
	req := &trillian.UpdateTreeRequest{
		Tree:       tree,
		UpdateMask: &field_mask.FieldMask{Paths: paths},
	}

	dialOpts, err := rpcflags.NewClientDialOptionsFromFlags()
	if err != nil {
		return nil, fmt.Errorf("failed to determine dial options: %w", err)
	}

	conn, err := grpc.Dial(*adminServerAddr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %v: %w", *adminServerAddr, err)
	}
	defer conn.Close()

	client := trillian.NewTrillianAdminClient(conn)
	for {
		tree, err := client.UpdateTree(ctx, req)
		if err == nil {
			return tree, nil
		}
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unavailable {
			glog.Errorf("Admin server unavailable, trying again: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil, fmt.Errorf("failed to UpdateTree(%+v): %T %w", req, err, err)
	}
}

func main() {
	flag.Parse()
	defer glog.Flush()

	ctx, cancel := context.WithTimeout(context.Background(), *rpcDeadline)
	defer cancel()
	if *treeID == 0 {
		fmt.Println("No tree ID passed in, returning")
		return
	}

	tree, err := updateTree(ctx)
	if err != nil {
		glog.Exitf("Failed to update tree: %v", err)
	}

	if *printTree {
		fmt.Println(prototext.Format(tree))
	} else {
		// DO NOT change the default output format, some scripts depend on it. If
		// you really want to change it, hide the new format behind a flag.
		fmt.Println(tree.TreeState)
	}
	fmt.Println("Completed successfully")
}
