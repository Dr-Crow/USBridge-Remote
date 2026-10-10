#!/usr/bin/env python3
"""Inspect only the disposable preview Xvfb. Saves numeric proof, never pixels."""
import argparse,ctypes as C,json,os,pathlib,time
p=argparse.ArgumentParser();p.add_argument('--mode',choices=['pixels','stop','gone'],required=True);p.add_argument('--output',required=True);a=p.parse_args()
assert os.environ.get('DISPLAY')==':97','only the owned preview acceptance display is allowed'
x=C.CDLL('libX11.so.6');xt=C.CDLL('libXtst.so.6')
x.XOpenDisplay.argtypes=[C.c_char_p];x.XOpenDisplay.restype=C.c_void_p;d=x.XOpenDisplay(b':97');assert d
x.XDefaultRootWindow.argtypes=[C.c_void_p];x.XDefaultRootWindow.restype=C.c_ulong;root=x.XDefaultRootWindow(d)
x.XQueryTree.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(C.c_ulong),C.POINTER(C.c_ulong),C.POINTER(C.POINTER(C.c_ulong)),C.POINTER(C.c_uint)]
x.XFetchName.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(C.c_void_p)];x.XFree.argtypes=[C.c_void_p]
x.XGetGeometry.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(C.c_ulong),C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_uint),C.POINTER(C.c_uint),C.POINTER(C.c_uint),C.POINTER(C.c_uint)]
x.XGetImage.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_int,C.c_uint,C.c_uint,C.c_ulong,C.c_int];x.XGetImage.restype=C.c_void_p
x.XGetPixel.argtypes=[C.c_void_p,C.c_int,C.c_int];x.XGetPixel.restype=C.c_ulong;x.XDestroyImage.argtypes=[C.c_void_p]
x.XTranslateCoordinates.argtypes=[C.c_void_p,C.c_ulong,C.c_ulong,C.c_int,C.c_int,C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_ulong)]
x.XSync.argtypes=[C.c_void_p,C.c_int];x.XCloseDisplay.argtypes=[C.c_void_p]
xt.XTestFakeMotionEvent.argtypes=[C.c_void_p,C.c_int,C.c_int,C.c_int,C.c_ulong];xt.XTestFakeButtonEvent.argtypes=[C.c_void_p,C.c_uint,C.c_int,C.c_ulong]
def windows():
 r=C.c_ulong();parent=C.c_ulong();children=C.POINTER(C.c_ulong)();count=C.c_uint()
 assert x.XQueryTree(d,root,C.byref(r),C.byref(parent),C.byref(children),C.byref(count))
 result=[]
 for i in range(count.value):
  name=C.c_void_p()
  if x.XFetchName(d,children[i],C.byref(name)) and name.value:
   title=C.string_at(name).decode('utf8','replace');x.XFree(name)
   if title=='Source preview (experimental, this computer)':result.append(children[i])
 if children:x.XFree(children)
 return result
def geometry(w):
 r=C.c_ulong();xx=C.c_int();yy=C.c_int();ww=C.c_uint();hh=C.c_uint();border=C.c_uint();depth=C.c_uint()
 assert x.XGetGeometry(d,w,C.byref(r),C.byref(xx),C.byref(yy),C.byref(ww),C.byref(hh),C.byref(border),C.byref(depth))
 return ww.value,hh.value
result={'mode':a.mode,'passed':False,'display':':97','pixels_saved':False};deadline=time.monotonic()+8
while time.monotonic()<deadline:
 matches=windows()
 if a.mode=='gone' and not matches:result['passed']=True;break
 if len(matches)!=1:time.sleep(.05);continue
 w=matches[0];ww,hh=geometry(w)
 if a.mode=='stop':
  xx=C.c_int();yy=C.c_int();child=C.c_ulong();assert x.XTranslateCoordinates(d,w,root,ww//2,hh-16,C.byref(xx),C.byref(yy),C.byref(child))
  assert xt.XTestFakeMotionEvent(d,-1,xx.value,yy.value,0);assert xt.XTestFakeButtonEvent(d,1,1,0);assert xt.XTestFakeButtonEvent(d,1,0,0);x.XSync(d,0);result['passed']=True;break
 if ww<200 or hh<150:time.sleep(.05);continue
 # Central grid excludes the title/status/Stop controls and contains actual
 # scaled image pixels; the blank viewer and canvas are not this blue.
 im=x.XGetImage(d,w,ww//2-80,hh//2-45,160,90,C.c_ulong(-1).value,2)
 if not im:time.sleep(.05);continue
 good=0;total=0
 for yy in range(5,90,10):
  for xx in range(5,160,10):
   v=x.XGetPixel(im,xx,yy);rgb=((v>>16)&255,(v>>8)&255,v&255);total+=1
   good+=all(abs(c-e)<=12 for c,e in zip(rgb,(22,76,180)))
 x.XDestroyImage(im)
 if good/total>=.95:result.update(passed=True,matching_samples=good,total_samples=total,window_width=ww,window_height=hh);break
 time.sleep(.05)
x.XCloseDisplay(d);pathlib.Path(a.output).write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result));assert result['passed'],result
