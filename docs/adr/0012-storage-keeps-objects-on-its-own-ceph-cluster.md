---
status: accepted
---

# Storage keeps objects on Nodera's own Ceph cluster, one bucket per Storage

Storage could have resold an object store the way Droplets resell a Provider's servers (ADR-0003).
It could not have hidden the supplier: software talks S3 straight to the store and signs the host
name into every request, so a resold store either shows its own address or has every byte proxied
and signed again by Nodera. So Storage runs on Nodera's own cluster, as Mail runs on its own MTA
(ADR-0009).

The engine is Ceph's RADOS Gateway (LGPL, Tentacle 20.2). We researched the engines on 2026-10-06.
Ceph is the one maintained engine that gives a hard quota per bucket, bucket policies, ACLs and
versioning, users with their own keys and policies, and a usage log of the bytes each bucket sent,
all behind an admin API a Go service drives over HTTP. MinIO's community edition is archived ("no
longer maintained" since 2026-02-12); Garage implements no ACLs, policies or versioning and counts
no traffic; SeaweedFS enforces a bucket's quota only from a periodic script; RustFS reached 1.0
three weeks before.

The storage service is the source of truth and Ceph enforces it. A Project is a Ceph account, a
Storage is one bucket in it with its Package as the bucket's quota, and an Access key is a user of
that account whose policy names the one Storage it reaches. No policy grants `s3:CreateBucket` or
`s3:DeleteBucket`: a bucket is a purchase, so it is opened and deleted only through Nodera.
Everything else Ceph implements of S3 stays open inside a Storage, publishing objects included.

An Access key is not the Project's API key (ADR-0010 wanted one place to come with a key). An API
key is a bearer secret Nodera keeps only as a hash; S3 signs each request with a secret the store
must hold itself, and S3 tools expect an access key ID and a secret key. So Storage has a second
kind of key, kept by Ceph and never by the customer context.

## Consequences

- Durability is Nodera's: three copies on three servers in one Region and no second site. The
  customer is told exactly that, and that it is not a backup; no durability figure and no SLA.
- The cluster costs the same empty or full. Cherry Servers has no storage-server line: on stock
  configurations three servers hold 8 TB at an estimated $0.12–0.19 a usable GB a month, against a
  market price of $0.007–0.02, and only custom servers with 16 TB disks come near it, at a price
  Cherry gives by quote. The one plan that takes those disks is in Lithuania, on a single 3 Gbps
  port that customers and replication share.
- So the software comes first, against a fake engine and a single-container Ceph. The production
  cluster is ordered once Cherry has quoted the disks and answered whether a custom server bills
  hourly, what a second network card costs and whether private traffic counts against the
  allowance.
- Ceph enforces a quota from cached statistics, so a Storage can pass its Package by what is
  uploaded in the minutes before Ceph notices. The excess is not charged.
- Ceph has no monthly cap on traffic. A Package measures only what is stored; traffic is recorded
  per Storage from the first day and each Storage is rate limited, because Cherry slows a server's
  port to 100 Mbps once the pooled allowance (100 TB a server) is passed. Selling transfer waits
  for real numbers.
- Published objects make Nodera the host of its customers' content, and Cherry may suspend without
  notice for what End Users store (its terms, 6.2 and 16.1). The Pause, an abuse@ mailbox and an
  operator's command are part of the first release. Ceph 20.2.4 has no command that stops one
  bucket, so how a Pause is enforced is settled in the contract test.
- A Storage's address is `<bucket name>.<region>.<domain>` and can never change. The Region is in
  it from the first Storage, Bucket names are unique in a Region across all Teams, and the domain
  is a registered domain of its own, never under the web app's, so that a complaint or a
  blocklisting of customer content cannot reach the web app.
- Static website hosting, bucket notifications and a customer's own domain are closed at first
  release, and so is opening or deleting a Storage with an API key.
- Ceph's demo image is unmaintained; dev and the contract test start the official image with a
  bootstrap script.
