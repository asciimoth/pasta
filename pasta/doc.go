// Package pasta provides the core model and lifecycle machinery for
// node-based editors and runtimes.
//
// A Workspace owns libraries, node classes, node instances, links, and ID
// generation. Applications register libraries that define classes, then use the
// workspace API to create nodes, replace ports, connect output ports to input
// ports, update editor metadata, and save or restore the graph. The package
// keeps the graph as a DAG and validates link direction, port existence, type
// compatibility, input multiplicity, and scoped ownership before committing
// mutations.
//
// The package does not interpret application behavior. Port type names,
// private node state, link objects, node coordinates, and link waypoints are
// stored and handed back to application code as contract values. Link objects
// are provided by the input-side runtime or by the caller during link creation,
// then passed to both endpoints through lifecycle hooks.
//
// Runtime callbacks run outside the workspace lock. They may call back into
// Workspace, LibraryScope, NodeScope, or WorkspaceRO according to the scope they
// were given. Before hooks can reject an operation and leave the graph
// unchanged; after hooks observe a committed state change. Panics from external
// library, class, or node code are recovered and logged through Logger.
//
// Inactive nodes and links are preserved when their endpoint model objects still
// exist, which lets editors display and recover graphs after a class recall,
// library unregister, or missing library on restore. Broken links whose
// endpoints or ports no longer exist are removed immediately.
// Classes may opt into single-node cardinality; create operations reject
// additional nodes of those classes, paste skips duplicate single-node class
// nodes, and restore keeps only the lowest-ID duplicate before initialization
// hooks run.
package pasta
