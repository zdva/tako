// Package transfer moves files between this computer and hosts, and between
// two hosts, over SFTP.
//
// A [Queue] runs [Spec] jobs a few at a time. Every copy streams from the
// source to the destination through memory (a 256 KiB-class buffer): a
// server-to-server copy through this computer never touches its disk. A file
// is written under a ".tako-part" name and renamed when complete, so the
// destination never holds half a file under its real name, and an
// interrupted copy continues from the bytes already there. A lost connection
// pauses the job instead of failing it.
//
// The optional direct mode asks the destination server to pull the file from
// the source with scp or rsync, using the user's SSH agent forwarded for that
// one command; no key is ever written to a server.
package transfer
