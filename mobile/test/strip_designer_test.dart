import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/unified/strip_designer.dart';
import 'package:pccontroller_mobile/unified/pixel_diagnostic.dart';

void main(){
  test('all presets support three physical pixels',(){for(var i=0;i<5;i++){final p=stripPreset(i);expect(stripProgramError(p),isNull);expect(stripPreview(p,3,1000),hasLength(3));}});
  test('invalid envelope is retained and cannot preview',(){final p=stripPreset(3);p['envelope'][1]['at_ms']=0;expect(stripProgramError(p),isNotNull);expect(stripPreview(p,3,0),isEmpty);expect(p['envelope'][1]['at_ms'],0);});
  test('fade and pulse timing differ',(){expect(stripPreview(stripPreset(3),3,500),isNot(stripPreview(stripPreset(4),3,500)));});
  test('probe clears every unselected pixel and rejects invalid bounds',(){final p=pixelProbe(38,37,[1,2,3]);expect(p,hasLength(114));expect(p.take(111).every((v)=>v==0),isTrue);expect(p.skip(111),[1,2,3]);expect(()=>pixelProbe(3,3,[255,0,0]),throwsArgumentError);expect(()=>pixelProbe(101,0,[255,0,0]),throwsArgumentError);});
  testWidgets('unapplied technical draft disables preset replacement',(tester)async{
    var changed=false;
    await tester.pumpWidget(MaterialApp(home:Scaffold(body:SingleChildScrollView(child:StripDesigner(effect:{'program':stripPreset(0),'default_pixels':3,'default_fps':20},enabled:false,changed:(_)=>changed=true)))));
    await tester.tap(find.text('Fade'));expect(changed,isFalse);
  });
}
