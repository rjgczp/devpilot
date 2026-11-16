"""Bounded fixture relay framing; drain coalesced stdin records immediately."""
import json,os
class Reader:
 def __init__(self,fd,limit=65536):self.fd=fd;self.limit=limit;self.pending=bytearray()
 def read(self):
  raw=os.read(self.fd,65536)
  if not raw:
   if self.pending:raise ValueError('truncated relay record')
   return None
  self.pending.extend(raw);out=[]
  while b'\n' in self.pending:
   line,_,rest=self.pending.partition(b'\n');self.pending=bytearray(rest)
   if len(line)>self.limit:raise ValueError('oversized relay record')
   out.append(json.loads(line))
  if len(self.pending)>self.limit:raise ValueError('oversized relay record')
  return out
