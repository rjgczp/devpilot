import json, unittest
from model import ModelBridge

class DemoModelTests(unittest.TestCase):
    def setUp(self):
        self.bridge = object.__new__(ModelBridge)
    def answer(self, rows):
        status, raw = self.bridge.mock({'messages':[{'role':'tool','tool_call_id':'get_recent_results','content':json.dumps({'data':rows})}]})
        self.assertEqual(status,200)
        return json.loads(json.loads(raw)['choices'][0]['message']['content'])
    def test_read_only_tools_are_requested_first(self):
        _, raw = self.bridge.mock({'messages':[]})
        tools = json.loads(raw)['choices'][0]['message']['tool_calls']
        self.assertEqual([t['function']['name'] for t in tools],['get_monitor','get_recent_results'])
        self.assertTrue(all(t['function']['arguments']=='{}' for t in tools))
    def test_evidence_uses_supplied_run_ids_and_measurements(self):
        rows=[{'id':f'actual-{i}','status':'success','http_status':200,'duration_ms':i+5} for i in range(3)]
        result=self.answer(rows)
        self.assertIn('Mock',result['summary'])
        self.assertEqual(result['confidence'],'low')
        self.assertEqual(len(result['evidence']),3)
        for row, text in zip(rows,result['evidence']):
            self.assertIn(row['id'],text)
            self.assertIn(str(row['duration_ms'])+' ms',text)
    def test_missing_or_failed_results_never_claim_success(self):
        for rows in [[],[{'id':'failed','status':'failure','http_status':500,'duration_ms':5}]]:
            with self.subTest(rows=rows):
                result=self.answer(rows)
                self.assertIn('证据不足或含非成功结果',result['possible_causes'][0])
                self.assertNotIn('均成功',result['possible_causes'][0])

if __name__=='__main__': unittest.main()
