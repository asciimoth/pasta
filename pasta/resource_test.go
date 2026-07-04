package pasta_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/asciimoth/pasta/pasta"
)

type trackedCloser struct {
	mu      sync.Mutex
	counter int
}

func (c *trackedCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counter += 1
	return nil
}

func (c *trackedCloser) count() int {
	time.Sleep(time.Millisecond * 10) // TODO: do not use sleep
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counter
}

type resourceInitNode struct {
	workspaceNode
	resource *trackedCloser
	err      error
}

func (n *resourceInitNode) OnInit(w *pasta.Workspace, l pasta.Logger, id uint64, class string, restored *pasta.NodeInitData, isReplacement bool, isPlaceholderReplacement bool, isClassConstructed bool, isRestored bool) error {
	if err := n.workspaceNode.OnInit(w, l, id, class, restored, isReplacement, isPlaceholderReplacement, isClassConstructed, isRestored); err != nil {
		return err
	}
	if err := w.AddNodeResourceLocked(id, n.resource); err != nil {
		return err
	}
	return n.err
}

type resourceLinkAddNode struct {
	workspaceNode
	w        *pasta.Workspace
	resource *trackedCloser
	err      error
}

func (n *resourceLinkAddNode) OnInit(w *pasta.Workspace, l pasta.Logger, id uint64, class string, restored *pasta.NodeInitData, isReplacement bool, isPlaceholderReplacement bool, isClassConstructed bool, isRestored bool) error {
	n.w = w
	return n.workspaceNode.OnInit(w, l, id, class, restored, isReplacement, isPlaceholderReplacement, isClassConstructed, isRestored)
}

func (n *resourceLinkAddNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	if err := n.workspaceNode.OnLinkAdd(link, port, linkType, portDirection); err != nil {
		return err
	}
	if err := n.w.AddLinkResourceLocked(link, n.resource); err != nil {
		return err
	}
	return n.err
}

func TestWorkspaceNodeResourcesCloseOnNodeLifecycleEnd(t *testing.T) {
	t.Run("RemoveNode", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		resource := &trackedCloser{}
		if err := w.AddNodeResource(nodeID, resource); err != nil {
			t.Fatalf("AddNodeResource: %v", err)
		}

		w.RemoveNode(nodeID)
		count := resource.count()
		if count != 1 {
			t.Fatalf("resource close count after RemoveNode = %d, want 1", count)
		}
		w.Close()
		if count != 1 {
			t.Fatalf("resource close count after workspace Close = %d, want 1", count)
		}
	})

	t.Run("ReplaceNode", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		resource := &trackedCloser{}
		if err := w.AddNodeResource(nodeID, resource); err != nil {
			t.Fatalf("AddNodeResource: %v", err)
		}

		if err := w.ReplaceNode(nodeID, &workspaceNode{}); err != nil {
			t.Fatalf("ReplaceNode: %v", err)
		}
		count := resource.count()
		if count != 1 {
			t.Fatalf("resource close count after ReplaceNode = %d, want 1", count)
		}
	})

	t.Run("ReplaceNode failure", func(t *testing.T) {
		failErr := errors.New("replace boom")
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		resource := &trackedCloser{}
		if err := w.AddNodeResource(nodeID, resource); err != nil {
			t.Fatalf("AddNodeResource: %v", err)
		}

		if err := w.ReplaceNode(nodeID, &workspaceNode{failOn: map[string]error{"OnReady": failErr}}); !errors.Is(err, failErr) {
			t.Fatalf("ReplaceNode error = %v, want %v", err, failErr)
		}

		count := resource.count()
		if count != 1 {
			t.Fatalf("resource close count after failed ReplaceNode = %d, want 1", count)
		}
	})

	t.Run("callback panic placeholder", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
		if err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		resource := &trackedCloser{}
		if err := w.AddNodeResource(nodeID, resource); err != nil {
			t.Fatalf("AddNodeResource: %v", err)
		}

		if err := w.ReplaceNode(nodeID, &workspaceNode{panicOn: map[string]bool{"OnInbox": true}}); err != nil {
			t.Fatalf("ReplaceNode: %v", err)
		}
		replacementResource := &trackedCloser{}
		if err := w.AddNodeResource(nodeID, replacementResource); err != nil {
			t.Fatalf("AddNodeResource replacement: %v", err)
		}

		w.SendInbox(pasta.InboxMessage{ReceiverNode: nodeID, Payload: "payload"})

		count := replacementResource.count()
		if count != 1 {
			t.Fatalf("resource close count after panic placeholder = %d, want 1", count)
		}
	})

	t.Run("OnInit failure after registration", func(t *testing.T) {
		failErr := errors.New("init boom")
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		resource := &trackedCloser{}

		nodeID, err := w.AddNode(&resourceInitNode{resource: resource, err: failErr}, "example.com/Node")
		if !errors.Is(err, failErr) {
			t.Fatalf("AddNode error = %v, want %v", err, failErr)
		}
		assertFailedPlaceholder(t, w, nodeID, "OnInit", "init boom")

		count := resource.count()
		if count != 1 {
			t.Fatalf("resource close count after failed OnInit = %d, want 1", count)
		}
	})
}

func TestWorkspaceLinkResourcesCloseOnLinkLifecycleEnd(t *testing.T) {
	t.Run("RemoveLink", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		_, _, leftPort, rightPort := addLinkedPairNodes(t, w, &workspaceNode{}, &workspaceNode{})
		linkID, _, err := w.AddLink(leftPort, rightPort)
		if err != nil {
			t.Fatalf("AddLink: %v", err)
		}
		resource := &trackedCloser{}
		if err := w.AddLinkResource(linkID, resource); err != nil {
			t.Fatalf("AddLinkResource: %v", err)
		}

		w.RemoveLink(linkID)

		count := resource.count()
		if count != 1 {
			t.Fatalf("resource close count after RemoveLink = %d, want 1", count)
		}
	})

	t.Run("RemoveNode removes attached links", func(t *testing.T) {
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		leftID, _, leftPort, rightPort := addLinkedPairNodes(t, w, &workspaceNode{}, &workspaceNode{})
		linkID, _, err := w.AddLink(leftPort, rightPort)
		if err != nil {
			t.Fatalf("AddLink: %v", err)
		}
		resource := &trackedCloser{}
		if err := w.AddLinkResource(linkID, resource); err != nil {
			t.Fatalf("AddLinkResource: %v", err)
		}

		w.RemoveNode(leftID)

		count := resource.count()
		if count != 1 {
			t.Fatalf("resource close count after RemoveNode = %d, want 1", count)
		}
	})

	t.Run("AddLink failure after callback registration", func(t *testing.T) {
		failErr := errors.New("link boom")
		w := pasta.NewWorkspace(&StringLoggerFactory{})
		resource := &trackedCloser{}
		leftID, err := w.AddNode(&resourceLinkAddNode{resource: resource}, "example.com/Left")
		if err != nil {
			t.Fatalf("AddNode left: %v", err)
		}
		rightID, err := w.AddNode(&workspaceNode{failOn: map[string]error{"OnLinkAdd": failErr}}, "example.com/Right")
		if err != nil {
			t.Fatalf("AddNode right: %v", err)
		}
		leftPort := mustAddPort(t, w, leftID, "left", "example.com/typeA")
		rightPort := mustAddPort(t, w, rightID, "right", "example.com/typeA")

		if _, _, err := w.AddLink(leftPort, rightPort); !errors.Is(err, failErr) {
			t.Fatalf("AddLink error = %v, want %v", err, failErr)
		}

		count := resource.count()
		if count != 1 {
			t.Fatalf("resource close count after failed AddLink = %d, want 1", count)
		}
	})
}

func TestWorkspaceResourcesCloseOnceWhenAnyOwnerCloses(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	leftID, rightID, leftPort, rightPort := addLinkedPairNodes(t, w, &workspaceNode{}, &workspaceNode{})
	linkID, _, err := w.AddLink(leftPort, rightPort)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	resource := &trackedCloser{}

	if err := w.AddNodeResource(leftID, resource); err != nil {
		t.Fatalf("AddNodeResource left: %v", err)
	}
	if err := w.AddNodeResource(leftID, resource); err != nil {
		t.Fatalf("duplicate AddNodeResource left: %v", err)
	}
	if err := w.AddNodeResource(rightID, resource); err != nil {
		t.Fatalf("AddNodeResource right: %v", err)
	}
	if err := w.AddLinkResource(linkID, resource); err != nil {
		t.Fatalf("AddLinkResource: %v", err)
	}

	w.RemoveNode(leftID)

	count := resource.count()
	if count != 1 {
		t.Fatalf("resource close count after first owner removal = %d, want 1", count)
	}
	w.RemoveNode(rightID)

	count = resource.count()
	if count != 1 {
		t.Fatalf("resource close count after second owner removal = %d, want 1", count)
	}
	w.Close()

	count = resource.count()
	if count != 1 {
		t.Fatalf("resource close count after workspace Close = %d, want 1", count)
	}
}

func TestWorkspaceCloseClosesTrackedResources(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	leftID, rightID, leftPort, rightPort := addLinkedPairNodes(t, w, &workspaceNode{}, &workspaceNode{})
	linkID, _, err := w.AddLink(leftPort, rightPort)
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	shared := &trackedCloser{}
	linkOnly := &trackedCloser{}

	if err := w.AddNodeResource(leftID, shared); err != nil {
		t.Fatalf("AddNodeResource left: %v", err)
	}
	if err := w.AddNodeResource(rightID, shared); err != nil {
		t.Fatalf("AddNodeResource right: %v", err)
	}
	if err := w.AddLinkResource(linkID, shared); err != nil {
		t.Fatalf("AddLinkResource shared: %v", err)
	}
	if err := w.AddLinkResource(linkID, linkOnly); err != nil {
		t.Fatalf("AddLinkResource linkOnly: %v", err)
	}

	w.Close()
	w.Close()

	count := shared.count()
	if count != 1 {
		t.Fatalf("shared resource close count after workspace Close = %d, want 1", count)
	}

	count = linkOnly.count()
	if count != 1 {
		t.Fatalf("link-only resource close count after workspace Close = %d, want 1", count)
	}
}

func TestWorkspaceResourceRegistrationErrors(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	resource := &trackedCloser{}

	if err := w.AddNodeResource(999, resource); !errors.Is(err, pasta.ErrNoNode) {
		t.Fatalf("AddNodeResource missing node error = %v, want %v", err, pasta.ErrNoNode)
	}
	if err := w.AddLinkResource(999, resource); !errors.Is(err, pasta.ErrNoLink) {
		t.Fatalf("AddLinkResource missing link error = %v, want %v", err, pasta.ErrNoLink)
	}

	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Node")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := w.AddNodeResource(nodeID, nil); !errors.Is(err, pasta.ErrNoResource) {
		t.Fatalf("AddNodeResource nil error = %v, want %v", err, pasta.ErrNoResource)
	}

	w.Close()
	if err := w.AddNodeResource(nodeID, resource); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddNodeResource closed error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
	if err := w.AddLinkResource(999, resource); !errors.Is(err, pasta.ErrWorkspaceClosed) {
		t.Fatalf("AddLinkResource closed error = %v, want %v", err, pasta.ErrWorkspaceClosed)
	}
}
