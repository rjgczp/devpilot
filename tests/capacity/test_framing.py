import os,sys,unittest
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parent))
from framing import Reader
class RelayFraming(unittest.TestCase):
 def test_guard_and_reply_coalesced_before_quiet_period(self):
  r,w=os.pipe()
  try:
   reader=Reader(r);os.write(w,b'{"guard":"pulse"}\n{"reply":{"event":"prepared"}}\n')
   self.assertEqual(reader.read(),[{'guard':'pulse'},{'reply':{'event':'prepared'}}])
   # Both records are handled without waiting for another kernel readability
   # event/pulse. A buffered readline+select relay can strand the second one.
   os.write(w,b'{"reply":');self.assertEqual(reader.read(),[])
   os.write(w,b'1}\n');self.assertEqual(reader.read(),[{'reply':1}])
   os.close(w);w=None;self.assertIsNone(reader.read())
  finally:
   os.close(r)
   if w is not None:os.close(w)
 def test_corrupt_or_partial_records_fail_closed(self):
  for raw in [b'{broken}\n',b'{"guard":']:
   r,w=os.pipe()
   try:
    reader=Reader(r);os.write(w,raw);os.close(w)
    with self.assertRaises((ValueError,TypeError)):
     while reader.read() is not None:pass
   finally:os.close(r)
if __name__=='__main__':unittest.main()
