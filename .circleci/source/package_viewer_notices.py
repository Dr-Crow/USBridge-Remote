#!/usr/bin/env python3
"""Retain the pinned native viewer dependencies' notices in its package."""
import argparse,pathlib,shutil
p=argparse.ArgumentParser();p.add_argument('client',type=pathlib.Path);p.add_argument('package',type=pathlib.Path);a=p.parse_args()
paths=['moonlight-common-c/LICENSE.txt','moonlight-common-c/enet/LICENSE','moonlight-common-c/nanors/LICENSE','third_party/pyrowave/vendor/pyrowave/LICENSE','third_party/pyrowave/vendor/pyrowave/PUNKTFUNK-VENDOR.txt','third_party/pyrowave/vendor/pyrowave/Granite/LICENSE','third_party/pyrowave/vendor/pyrowave/Granite/third_party/volk/LICENSE.md','third_party/pyrowave/vendor/pyrowave/Granite/third_party/khronos/vulkan-headers/LICENSE.md']
license_dir=a.client/'third_party/pyrowave/vendor/pyrowave/Granite/third_party/khronos/vulkan-headers/LICENSES'
assert license_dir.is_dir() and not license_dir.is_symlink()
paths += [p.relative_to(a.client).as_posix() for p in sorted(license_dir.rglob('*')) if p.is_file()]
assert len(paths)>8
for rel in paths:
 source=a.client/rel;assert source.is_file() and not source.is_symlink() and source.stat().st_size<=1<<20,rel
 target=a.package/'source/viewer-notices'/rel;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(source,target)
print('Retained',len(paths),'native dependency notice/provenance files')
